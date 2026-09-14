# Monitoramento VMware / ESXi

O perfil `vmware` coleta inventário, capacidade de host e datastore, estado, CPU, memória, tráfego de rede, throughput, IOPS e latência de disco das VMs por SOAP API do ESXi ou vCenter. Ele não instala agente no hypervisor e o endpoint Prometheus fica somente na rede Docker.

Crie no VMware uma conta exclusiva com papel **ReadOnly** na raiz do inventário e propagação. No servidor SentinelOps, guarde a senha fora do repositório em arquivo root:root 0600. Configure no `.env`: `VMWARE_ENDPOINT`, `VMWARE_USERNAME`, `SENTINEL_VMWARE_PASSWORD_FILE` e `VMWARE_TLS_THUMBPRINT` (SHA-256 do certificado). O exporter exige endpoint HTTPS sem credenciais, query ou fragmento e thumbprint hexadecimal completo; não existe modo de ignorar TLS.

Inicie com `docker compose --env-file .env -f deploy/compose/docker-compose.yml --profile vmware up -d --build vmware-exporter`. Valide `http://127.0.0.1:9472/healthz`, o target `sentinel-vmware`, as séries `sentinelops_vmware_*` e os alertas VMware antes do aceite.

O dashboard **VMware Virtual Machine Performance** reúne RX/TX de rede, leitura/escrita de disco, IOPS, latência, CPU, memória, espaço livre e o estado do VMware Tools. As métricas representam a visão do hypervisor. Para processos, filesystem e serviços dentro da VM, instale o bootstrap de Linux ou Windows no sistema convidado.

## Reconciliação opcional no catálogo

Um collector/agente já provisionado com capability `inventory:write` pode
publicar um snapshot sanitizado por `SENTINEL_INVENTORY_FILE`. O arquivo é
limitado a 4 MiB e deve conter `complete: true`; tenant e fonte vêm da
credencial do agente, não do arquivo.

```json
{
  "complete": true,
  "observedAt": "2026-09-09T15:00:00Z",
  "assets": [
    {"assetId": "vm:423b0e7a", "name": "app-01", "kind": "vm", "site": "tqi-sp"}
  ]
}
```

Use o UUID estável do vSphere, não IP ou hostname. O snapshot precisa
representar a fonte inteira: VMs ausentes são marcadas `stale` somente após a
persistência atômica. Não inclua IPs públicos, credenciais, notas de guest ou
dados pessoais em labels.
