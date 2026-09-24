#!/usr/bin/env python3
"""Admissão e drenagem do edge Nginx sem reiniciá-lo ou encerrar conexões.

Uso como biblioteca pelo procedimento de release. Nunca para containers: a
barreira devolve o controle somente quando a geração anterior terminou.
"""
from __future__ import annotations
import hashlib
import ipaddress
import os
from pathlib import Path
import re
import subprocess
import time

class DrainTimeout(RuntimeError):
    """O operador deve manter todas as réplicas vivas."""

def output(args):
    try:
        return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT)
    except subprocess.CalledProcessError as exc:
        raise RuntimeError(exc.output) from exc

class EdgeDrainer:
    def __init__(self, container: str, config: Path, evidence: Path, probe, guard=lambda: None, timeout=120):
        self.container, self.config, self.evidence = container, config, evidence
        self.probe, self.guard, self.timeout = probe, guard, timeout
        self.original = config.read_bytes()
        self.last = self.original
        self.generation = 0
        self.admitted = set()
        evidence.mkdir(parents=True, exist_ok=True)

    def workers(self):
        lines = output(['docker', 'top', self.container, '-eo', 'pid,lstart,args']).splitlines()[1:]
        return {tuple(line.split()[:6]) for line in lines if 'nginx: worker process' in line}

    def _write_same_inode(self, data):
        # O bind mount atual referencia um arquivo: os.replace trocaria o inode.
        with self.config.open('r+b') as target:
            target.seek(0); target.write(data); target.truncate(); target.flush(); os.fsync(target.fileno())
        expected = hashlib.sha256(data).hexdigest()
        actual = output(['docker','exec',self.container,'sha256sum','/etc/nginx/conf.d/default.conf']).split()[0]
        if actual != expected:
            raise RuntimeError('edge não enxerga os mesmos bytes de configuração')

    def _validate_candidate(self, data):
        candidate = self.evidence / 'candidate.conf'
        candidate.write_bytes(b'pid /var/run/nginx.pid; events {} http {\n'+data+b'\n}\n')
        candidate.chmod(0o644)
        subprocess.run(['docker','exec','-i',self.container,'sh','-c','cat > /var/cache/nginx/sentinel-candidate.conf'],input=candidate.read_bytes(),check=True)
        output(['docker','exec',self.container,'nginx','-t','-c','/var/cache/nginx/sentinel-candidate.conf'])

    def apply(self, data: bytes):
        self.guard()
        if self.config.read_bytes() != self.last:
            raise RuntimeError('configuração edge mudou fora desta transação')
        self._validate_candidate(data)
        before = self.workers()
        if not before:
            raise RuntimeError('não foi possível identificar workers Nginx')
        previous = self.last
        try:
            self._write_same_inode(data)
            output(['docker','exec',self.container,'nginx','-t'])
        except Exception:
            self._write_same_inode(previous)
            raise
        self.last = data
        self.generation += 1
        (self.evidence / f'generation-{self.generation}.conf').write_bytes(data)
        output(['docker','exec',self.container,'nginx','-s','reload'])
        deadline = time.monotonic()+self.timeout
        new_seen = False
        while time.monotonic() < deadline:
            current = self.workers()
            new_seen = new_seen or bool(current-before)
            self.probe()
            self.guard()
            if new_seen and current and not current.intersection(before):
                (self.evidence / f'generation-{self.generation}.drained').write_text('workers anteriores encerrados; nova geração respondeu\n')
                return
            time.sleep(.2)
        raise DrainTimeout('drenagem excedeu prazo; manter todas as APIs vivas, sem docker stop')

    def pin(self, ips: list[str]):
        if not 1 <= len(ips) <= 3 or len(set(ips)) != len(ips):
            raise ValueError('conjunto de upstream inválido')
        for ip in ips:
            if ipaddress.ip_address(ip).version != 4:
                raise ValueError('upstream precisa ser IPv4 confirmado pelo Docker')
        # Um nome por geração evita compartilhar peers mutáveis entre workers.
        name = f'sentinel_api_release_{self.generation+1}'
        text = self.original.decode()
        block = 'upstream '+name+' {\n'+''.join('  server '+ip+':8080 max_fails=1 fail_timeout=1s;\n' for ip in ips)+'}\n'
        text, count = re.subn(r'upstream sentinel_api_ha\s*\{[^}]+\}', block, text, count=1)
        if count != 1:
            raise ValueError('upstream esperado não encontrado')
        text = text.replace('proxy_pass http://sentinel_api_ha;', 'proxy_pass http://'+name+';')
        text = 'log_format sentinel_release \'$time_iso8601 $status $upstream_status $upstream_addr $request_time\';\naccess_log /dev/stdout sentinel_release;\n'+text
        self.apply(text.encode())
        self.admitted = set(ips)

    def restore_dynamic(self):
        self.apply(self.original)
