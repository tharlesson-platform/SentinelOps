# Monitoramento VMware / ESXi

O perfil `vmware` coleta inventário, capacidade de host e datastore, estado, CPU e memória das VMs por SOAP API do ESXi ou vCenter. Ele não instala agente no hypervisor e o endpoint Prometheus fica somente na rede Docker.

Crie no VMware uma conta exclusiva com papel **ReadOnly** na raiz do inventário e propagação. No servidor SentinelOps, guarde a senha fora do repositório em arquivo root:root 0600. Configure no `.env`: `VMWARE_ENDPOINT`, `VMWARE_USERNAME`, `SENTINEL_VMWARE_PASSWORD_FILE` e `VMWARE_TLS_THUMBPRINT` (SHA-256 do certificado). A impressão é obrigatória: não existe modo de ignorar TLS.

Inicie com `docker compose --env-file .env -f deploy/compose/docker-compose.yml --profile vmware up -d --build vmware-exporter`. Valide `http://127.0.0.1:9472/healthz`, o target `sentinel-vmware` e as séries `sentinelops_vmware_*` antes do aceite.
