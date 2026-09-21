#!/usr/bin/env python3
"""Release transacional da API/web no host SentinelOps, com drenagem do edge.

Requer release imutável com source/, web/, out/app e commit.txt. Execute prepare,
revise as evidências e então deploy. Timeout de drain preserva todas as réplicas.
"""
from __future__ import annotations
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from api_edge_drain import EdgeDrainer, DrainTimeout

ROOT=Path('/opt/sentinelops')
RELEASE=Path(sys.argv[2]).resolve() if len(sys.argv)>2 else Path.cwd()
BACKUP=RELEASE/'backup'
LOCK=ROOT/'artifacts/runtime/docker-compose.images.lock.yml'
WEB=ROOT/'private-ingress/web/sentinelops'
EDGE_CONFIG=ROOT/'deploy/compose/api-edge.nginx.conf'
EDGE='sentinelops-api-edge-1'
COMPOSE=['docker','compose','--env-file',str(ROOT/'.env'),'-f',str(ROOT/'deploy/compose/docker-compose.yml'),'-f',str(ROOT/'deploy/compose/docker-compose.ha.yml'),'-f',str(LOCK)]
BASE='https://sentinelops.tqi.com.br/sentinelops'

def run(args):return subprocess.check_output(args,text=True,stderr=subprocess.STDOUT)
def inspect(ids):return json.loads(run(['docker','inspect',*ids])) if ids else []
def apis():return inspect(run(['docker','ps','-aq','--filter','label=com.docker.compose.project=sentinelops','--filter','label=com.docker.compose.service=api']).split())
def ip(item):return item['NetworkSettings']['Networks']['sentinelops_control']['IPAddress']
def good(item):return item['State']['Status']=='running' and item['State'].get('Health',{}).get('Status')=='healthy'
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def atomic(path,data,mode=0o644):
 tmp=path.with_name('.'+path.name+'.release.tmp');tmp.write_bytes(data);tmp.chmod(mode);os.replace(tmp,path)
def safe_meta(items):return [{'id':x['Id'],'name':x['Name'],'image':x['Image'],'health':x['State'].get('Health',{}).get('Status'),'restarts':x['RestartCount']} for x in items]
def read_json(path):return json.loads(path.read_text())
def save(name,obj):(RELEASE/name).write_text(json.dumps(obj,indent=2))
def get(url):
 with urllib.request.urlopen(url,timeout=10) as r:
  if r.status!=200:raise RuntimeError('HTTP diferente de 200')
  return r.read()
def probe():
 # O ingress publica healthz; readyz sob o subpath cairia no fallback HTML da SPA.
 data=json.loads(get(BASE+'/healthz'))
 assert data.get('data',{}).get('status')=='ok' and data['data'].get('time'),'healthz não retornou envelope da API'
def auth(origin):
 env={}
 for line in (ROOT/'.env').read_text().splitlines():
  if '=' in line and not line.lstrip().startswith('#'):
   k,v=line.split('=',1)
   if k in ('LOCAL_ADMIN_USER','LOCAL_ADMIN_PASSWORD'):env[k]=v.strip().strip('"\'')
 body=json.dumps({'username':env.get('LOCAL_ADMIN_USER','admin'),'password':env.get('LOCAL_ADMIN_PASSWORD','')}).encode()
 req=urllib.request.Request(origin+'/api/v1/auth/login',body,{'Content-Type':'application/json'})
 with urllib.request.urlopen(req,timeout=15) as r:
  assert r.status==200 and json.load(r).get('data',{}).get('accessToken'),'autenticação não validada'
def set_image(image):
 text,count=re.subn(r'(?m)^(  api: \{ image: )"[^"]+"',lambda m:m[1]+'"'+image+'"',LOCK.read_text());assert count==1
 atomic(LOCK,text.encode(),0o600)
def scale3():run(COMPOSE+['up','-d','--no-deps','--no-build','--pull','never','--no-recreate','--scale','api=3','api'])
def wait_healthy(image,count,guard):
 deadline=time.monotonic()+150
 while time.monotonic()<deadline:
  guard();items=[x for x in apis() if x['Image']==image and good(x)]
  if len(items)>=count:
   for x in items:get('http://'+ip(x)+':8080/readyz');auth('http://'+ip(x)+':8080')
   return items
  time.sleep(1)
 raise RuntimeError('prontidão da API excedeu prazo')
def remove(item):
 now=inspect([item['Id']])[0];assert now['Image']==item['Image'];run(['docker','stop','-t','30',item['Id']]);run(['docker','rm',item['Id']])
def resolved_api_ips():
 # BusyBox getent hosts retorna somente um peer; nslookup expõe o RRset A completo.
 answer=run(['docker','exec',EDGE,'nslookup','-type=A','api','127.0.0.11'])
 return set(re.findall(r'(?m)^Address:\s+(\d+\.\d+\.\d+\.\d+)\s*$',answer))
def replace_replicas(image,edge,guard):
 current=apis();wanted=[x for x in current if good(x) and x['Image']==image]
 # Healthy não equivale a aprovada: uma candidata rejeitada pelo gate nunca entra no rollback.
 initial=wanted[:2] if len(wanted)>=2 else [x for x in current if good(x) and ip(x) in edge.admitted]
 assert len(initial)>=2,"duas réplicas já admitidas exigidas para iniciar troca"
 edge.pin([ip(x) for x in initial])
 # A lista elegível é atualizada e drenada ANTES de cada parada, inclusive rollback.
 while True:
  current=apis();wanted=[x for x in current if x['Image']==image and good(x)]
  if len(wanted)>=2:
   selected=wanted[:2];edge.pin([ip(x) for x in selected]);ids={x['Id'] for x in selected}
   for x in current:
    if x['Id'] not in ids:guard();remove(x)
   break
  if len(current)>=3:
   victims=[x for x in current if x['Image']!=image or not good(x)]
   assert victims,'nenhuma réplica segura para retirar'
   victim=next((x for x in victims if ip(x) not in edge.admitted),victims[-1]);keepers=[x for x in current if x['Id']!=victim['Id'] and good(x)]
   assert len(keepers)>=2,'menos de duas réplicas prontas para retirar excedente'
   edge.pin([ip(x) for x in keepers]);guard();remove(victim)
  before=len(wanted);scale3();wanted=wait_healthy(image,before+1,guard)
  if before==0 and image==read_json(RELEASE/'prepared.json')['new_image']:
   subprocess.run(['python3',str(RELEASE/'verify-coverage.py'),'http://'+ip(wanted[0])+':8080',str(RELEASE/'candidate-coverage.json')],check=True)
  current=apis();victims=[x for x in current if x['Image']!=image]
  if victims:
   victim=victims[0];keepers=[x for x in current if x['Id']!=victim['Id'] and good(x)]
   assert len(keepers)>=2;edge.pin([ip(x) for x in keepers]);guard();remove(victim)
 final=wait_healthy(image,2,guard);assert len(apis())==2
 expected={ip(x) for x in final};deadline=time.monotonic()+15
 while time.monotonic()<deadline:
  resolved=resolved_api_ips()
  if resolved==expected:break
  time.sleep(.2)
 else:raise RuntimeError('DNS api não convergiu para as duas réplicas confirmadas')
 edge.restore_dynamic();probe();auth(BASE)
 return final

def prepare():
 assert (RELEASE/'out/app').read_bytes()[:4]==b'\x7fELF'
 current=apis();assert len(current)==2 and all(good(x) for x in current)
 assert len({x['Image'] for x in current})==1
 assert (ROOT/'DEPLOYED_COMMIT').read_text().strip()==(RELEASE/'base-commit.txt').read_text().strip(),'base mudou'
 assert all(set(x['NetworkSettings']['Networks'])=={'sentinelops_control','sentinelops_telemetry'} for x in current)
 probe();auth(BASE);run(COMPOSE+['config','--quiet']);run(['docker','exec',EDGE,'nginx','-t'])
 subprocess.run(['python3',str(RELEASE/'validate-runtime.py'),str(RELEASE/'before-runtime.json'),'baseline'],check=True)
 assert all(x['passed'] for x in read_json(RELEASE/'before-runtime.json')['checks'].values()),'baseline funcional não aprovada'
 assert int(next(x.split()[1] for x in Path('/proc/meminfo').read_text().splitlines() if x.startswith('MemAvailable:')))>1024*1024
 BACKUP.mkdir(mode=0o700)
 shutil.copytree(WEB,BACKUP/'web');shutil.copy2(LOCK,BACKUP/'images.lock.yml');shutil.copy2(EDGE_CONFIG,BACKUP/'edge.conf');shutil.copy2(ROOT/'DEPLOYED_COMMIT',BACKUP/'DEPLOYED_COMMIT')
 all_containers=inspect(run(['docker','ps','-q']).split());save('before-containers.json',safe_meta(all_containers));save('before-api.json',safe_meta(current))
 commit=(RELEASE/'commit.txt').read_text().strip();assert re.fullmatch('[a-f0-9]{40}',commit)
 old=current[0]['Image'];image='sentinelops-api:coverage-'+commit[:7]
 # BuildKit interpreta FROM sha256:... como repositório, não como ID local.
 base_tag='sentinelops-api:rollback-'+commit[:7]
 run(['docker','tag',old,base_tag])
 assert json.loads(run(['docker','image','inspect',base_tag]))[0]['Id']==old
 (RELEASE/'out/Dockerfile').write_text('FROM '+base_tag+'\nCOPY --chown=10001:10001 --chmod=0555 app /usr/local/bin/app\nLABEL org.opencontainers.image.revision="'+commit+'"\n')
 built=subprocess.run(['docker','build','--network=none','--pull=false','-t',image,str(RELEASE/'out')],text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 (RELEASE/'image-build.log').write_text(built.stdout)
 built.check_returncode()
 new=json.loads(run(['docker','image','inspect',image]))[0];prior=json.loads(run(['docker','image','inspect',old]))[0]
 for key in ('User','Entrypoint','Cmd','Env'):assert new['Config'].get(key)==prior['Config'].get(key),'runtime mudou: '+key
 assert new['Architecture']=='amd64'
 save('prepared.json',{'commit':commit,'base_image':old,'new_image':new['Id'],'binary_sha256':sha(RELEASE/'out/app'),'runtime_config_sha256':sha(WEB/'config.js'),'environment_sha256':sha(ROOT/'.env')})
 print('RELEASE_PREPARED',commit,flush=True)

def publish_web():
 for path in (RELEASE/'web').rglob('*'):
  if path.is_file() and path.name not in ('index.html','config.js'):
   dst=WEB/path.relative_to(RELEASE/'web');dst.parent.mkdir(parents=True,exist_ok=True);dst.parent.chmod(0o755);atomic(dst,path.read_bytes())
 atomic(WEB/'index.html',(RELEASE/'web/index.html').read_bytes())
 for path in (RELEASE/'web').rglob('*'):
  if path.is_file() and path.name!='config.js':assert hashlib.sha256(get(BASE+'/'+str(path.relative_to(RELEASE/'web')))).hexdigest()==sha(path),'asset divergente'
 assert sha(WEB/'config.js')==sha(BACKUP/'web/config.js')

def deploy():
 assert all(x['passed'] for x in read_json(RELEASE/'before-runtime.json')['checks'].values()),'baseline funcional ausente ou reprovada'
 prepared=read_json(RELEASE/'prepared.json');assert LOCK.read_bytes()==(BACKUP/'images.lock.yml').read_bytes();assert EDGE_CONFIG.read_bytes()==(BACKUP/'edge.conf').read_bytes()
 current=apis()
 assert {x['Id'] for x in current}=={x['id'] for x in read_json(RELEASE/'before-api.json')}
 assert all(good(x) and x['Image']==prepared['base_image'] and set(x['NetworkSettings']['Networks'])=={'sentinelops_control','sentinelops_telemetry'} for x in current),'baseline API mudou'
 assert (ROOT/'DEPLOYED_COMMIT').read_bytes()==(BACKUP/'DEPLOYED_COMMIT').read_bytes()
 assert sha(ROOT/'.env')==prepared['environment_sha256']
 assert all(sha(WEB/name)==sha(BACKUP/'web'/name) for name in ['index.html','config.js']),'web runtime mudou'
 probe();auth(BASE)
 observations=[];done=threading.Event();failed=threading.Event()
 def monitor():
  while not done.is_set():
   start=time.monotonic();row={'utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())}
   try:probe();row['status']=200
   except urllib.error.HTTPError as e:row.update(status=e.code,error='HTTPError');failed.set()
   except Exception as e:row.update(status=None,error=type(e).__name__);failed.set()
   row['ms']=round((time.monotonic()-start)*1000);observations.append(row);done.wait(.3)
 def guard():
  if failed.is_set():raise RuntimeError('monitor detectou falha durante rollout')
 edge=EdgeDrainer(EDGE,EDGE_CONFIG,RELEASE/'edge-evidence',probe,guard)
 thread=threading.Thread(target=monitor);thread.start();mutated=False;api_changed=False
 try:
  mutated=True
  edge.pin([ip(x) for x in apis()])
  set_image(prepared['new_image']);api_changed=True
  final=replace_replicas(prepared['new_image'],edge,guard)
  publish_web();probe();auth(BASE);guard()
  subprocess.run(['python3',str(RELEASE/'validate-runtime.py'),str(RELEASE/'after-runtime.json')],check=True)
  prior={x['name']:x['id'] for x in read_json(RELEASE/'before-containers.json') if x['id'] not in {a['id'] for a in read_json(RELEASE/'before-api.json')}}
  now={x['Name']:x['Id'] for x in inspect(run(['docker','ps','-q']).split())};assert all(now.get(k)==v for k,v in prior.items()),'outro container mudou'
  guard();atomic(ROOT/'DEPLOYED_COMMIT',(prepared['commit']+'\n').encode());save('deployed.json',{'commit':prepared['commit'],'replicas':safe_meta(final),'unrelated_containers_preserved':len(prior),'runtime_config_preserved':True,'source_archive':str(RELEASE/'source')});print('DEPLOYED',prepared['commit'],flush=True)
 except DrainTimeout:
  print('DRAIN_TIMEOUT_ALL_REPLICAS_PRESERVED',flush=True);raise
 except Exception:
  if mutated:
   edge.guard=lambda:None
   if api_changed:
    set_image(prepared['base_image']);replace_replicas(prepared['base_image'],edge,lambda:None)
   else:
    edge.restore_dynamic()
   atomic(LOCK,(BACKUP/'images.lock.yml').read_bytes(),0o600);atomic(WEB/'index.html',(BACKUP/'web/index.html').read_bytes());atomic(ROOT/'DEPLOYED_COMMIT',(BACKUP/'DEPLOYED_COMMIT').read_bytes())
   print('ROLLBACK_COMPLETED',flush=True)
  raise
 finally:
  done.set();thread.join();save('http-monitor.json',observations)
  print('MONITOR',json.dumps({'samples':len(observations),'failures':sum(x['status']!=200 for x in observations)}),flush=True)

if __name__=='__main__':
 os.umask(0o077)
 assert os.geteuid()==0 and Path('/proc/sys/kernel/hostname').read_text().strip()=='sentinelops'
 assert RELEASE.is_relative_to(ROOT/'releases')
 with (ROOT/'artifacts/runtime/deployment.lock').open('a') as lock:
  fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
  {'prepare':prepare,'deploy':deploy}[sys.argv[1]]()
