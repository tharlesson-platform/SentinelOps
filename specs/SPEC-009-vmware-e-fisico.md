# SPEC-009 — VMware e infraestrutura física

**Fase:** E3 · **Estado:** IN_PROGRESS · **Responsável:** Virtualização/SRE

## Problema, escopo e contratos

O exporter govmomi e conectores físicos precisam reconciliar vCenter/ESXi,
datacenters, clusters, hosts, VMs, datastores, snapshots, Tools, alarmes e
eventos com IDs estáveis. CPU ready/co-stop, balloon/swap, IOPS, latência,
throughput e rede só aparecem quando o contador existe e tem unidade conhecida.

## Dados, UX, autorização e operação

Credenciais são read-only e o checkpoint/paginação evita carga excessiva.
Ausência de licença, permissão ou contador permanece explícita no catálogo e
na evidência. Redfish/IPMI/storage/UPS nunca enviam comandos de energia.

## Critérios e evidência

- **AC-0901:** Dadas reconciliações repetidas, quando hostname muda, então a
  VM preserva asset ID e histórico.
- **AC-0902:** Dado contador selecionado, quando comparado à API/console, então
  valor, timestamp e unidade correspondem à amostra.
- **AC-0903:** Dada permissão insuficiente, quando o conector executa, então
  erro é visível sem converter contador ausente em zero.
- **AC-0904:** Dada condição degradada segura, quando ocorre, então sinal,
  alerta e overhead da sessão são registrados.

O exporter existente passou a rejeitar endpoint sem HTTPS, com credenciais,
query/fragment e thumbprint não hexadecimal; também recusa arquivo de senha
legível por grupo/outros. Esses contratos têm teste Go local. Homologação
requer vCenter autorizado. Rollout começa por datacenter não crítico; rollback
para o exporter anterior sem apagar inventário.
