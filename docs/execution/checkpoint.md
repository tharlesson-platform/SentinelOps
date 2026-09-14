# Checkpoint de retomada

**Concluído:** HEAD reconciliado; ledger de migrations; UI sem dados fixos;
API de resultados sintéticos; assertions e política hostname+CIDR; claim
durável validado em PostgreSQL isolado; inbox/outbox, retry/DLQ e SSE com
cursor implementados; incidentes deduplicados, ACK/resolução e auditoria
atômica implementados; contrato OIDC agora valida audience e escopo de API,
preserva `sub` e aplica `role_bindings` autoritativo.
Gates de release passaram a exigir janela temporal, amostras recentes e
cobertura positiva de traces; dados parciais, instantâneos e não-finitos não
aprovam promoção.
Inventário inicial de ativos agora possui `assetId` estável, lifecycle,
source, upsert idempotente e RLS; falta conectar fontes autorizadas.
O endpoint de reconciliação aceita snapshots completos de fontes read-only e
marca ausências como stale atomicamente.
Agentes com capability `inventory:write` podem publicar esses snapshots sem
token de usuário; o tenant e a fonte são vinculados à credencial do agente.
O agente publica opcionalmente `SENTINEL_INVENTORY_FILE` completo e
sanitizado; conectores ainda precisam gerar o snapshot a partir de fontes
autorizadas.
Administradores agora podem revogar agentes; heartbeat e inventário recusam
imediatamente credenciais revogadas.
No perfil produtivo, o worker de validação não consulta telemetria diretamente:
ele exige gateway HTTPS com certificado mTLS próprio e o Helm retira o egress
às portas dos backends. Ainda falta homologar a CA e os backends
multi-tenant no alvo autorizado.
Escalonamento de incidente agora é agendado de forma durável, dispara target
secundário somente enquanto OPEN e é cancelado por ACK/resolução.
O catálogo possui busca server-side limitada e paginada para UI, CLI e MCP;
o servidor MCP por stdio é read-only, delega à API/RBAC e suporta TLS/mTLS sem
acesso ao banco. O guardrail pré-provider de IA possui 30 cenários sanitizados
e abstém-se por padrão; nenhum modelo ou ferramenta é chamado nesse corte.
O smoke E2E local passou com login/dashboard consultando a API, sem erros de
console no browser; o k6 executou 444 checks sem falhas e p95 de 5,17 ms.
Isso não habilita browser/k6 distribuído nem comprova ambiente externo.
O corte Windows possui templates Alloy, bookmarks persistentes, instalação
silenciosa por artefato local verificado, ACLs e packs protegidos por role;
PowerShell e coleta em host TQI ainda não foram executados.
O corte inicial de rede usa SNMPv3 authPriv, allowlist de IP literal e exporter
sem porta publicada. O perfil syslog UDP/TCP requer flag explícita, bind IP
unicast e Loki mTLS; sua sintaxe foi validada localmente. Ainda não houve acesso
à VLAN/VRF, FortiGate, emissor real ou listener de traps autorizado.
O exporter VMware agora rejeita endpoint inseguro, thumbprint inválido e senha
com permissão de grupo/outros; coleta e comparação com vCenter autorizado
continuam pendentes.
O corte cloud agora gera snapshots read-only por uma única subscription Azure ou
account/região AWS: valida a identidade efetiva, pagina e troca o arquivo de
forma atômica. Azure usa Resource Graph; AWS exige AWS Config gravando todos os
tipos suportados e recusa resultado fora do scope. O agente pode bloquear
snapshot vencido por `SENTINEL_INVENTORY_MAX_AGE`. Ainda não há conta/subscription
autorizada, testes de 429/expiração, métricas/logs cloud ou revisão de RBAC
efetivo; nenhum dos dois conectores foi homologado externamente.
O corte Kubernetes agora inventaria nodes e recursos de namespaces explícitos
com UID estável e snapshot atômico. Ele exige `list` somente para recursos
coletados e falha se `get secrets` ou `create deployments` for permitido. O
RBAC cluster mínimo e os RoleBindings por namespace são renderizados; ainda
faltam cluster isolado, ServiceAccount projetada, eventos, métricas, logs,
traces e bancos de homologação.
O corte PostgreSQL expõe estatísticas agregadas read-only de conexões, waits,
locks e deadlocks, por DSN TLS em arquivo 0600 e com readiness baseada em
coleta recente. Ainda faltam role `pg_monitor` homologada, scrape privado,
alerta de lock/saturação e os packs MySQL, SQL Server, Redis e filas.
O exporter PostgreSQL agora também é um profile Compose explícito e participa
de build/lock/assinatura local de imagens; continua desligado sem DSN TLS
montado, portanto não há coleta local simulada nem liberação de produção. O
scrape privado e as regras de stale, saturação, lock e deadlock passam em
`promtool`, mas ainda não foram exercitados contra banco nem canal externo.
O onboarding APM agora restringe HTTP a loopback e rejeita credenciais/query em
endpoints, enquanto o harness prova a geração do kit e os casos de negação.
Ainda faltam aplicação TQI, jornada real de três serviços, PII por runtime e
baseline de overhead.
O kit React/Faro agora exige HTTPS, sampling explícito e remove query/fragmento
da URL de página no `beforeSend`, ignorando o próprio collector; Session Replay
permanece desligado. Receiver, CORS/rate limit, jornada frontend→backend,
revisão LGPD, flows, eBPF e forecast com incerteza ainda não existem.
O restore local agora recusa reutilizar um projeto Compose com recursos
preexistentes, autentica e valida o contrato do pacote antes de iniciar serviços
e persiste hash, duração e contagens em evidência sem segredos. A execução de
restore, RTO/RPO, PITR, recuperação de CA/segredos e DR regional ainda precisam
de janela, owner e alvo autorizado.
O control plane possui agora um teto inicial por organização autenticada, com
429/Retry-After, métrica e alerta. A janela é um UPSERT PostgreSQL atômico
tenant-scoped, compartilhado entre réplicas; faltam quotas por
sinal/query/storage/IA/MCP e orçamento cloud. Solicitações de exportação ou
exclusão agora são tenant-scoped, auditadas no mesmo commit e exigem aprovação
de outro Platform Administrator; elas não executam retenção, exportação ou
exclusão até existir executor homologado por backend e política aprovada.
O único executor localmente validado é o erasure de identidade do control plane:
redige perfil e remove role bindings depois de aprovação e confirmação; não
altera auditoria histórica, telemetria ou outros backends.
As consultas de catálogo agora têm quota durável adicional por organização e
rota, inclusive quando delegadas pelo MCP; ainda não há orçamento por séries,
bytes, storage, ingestão ou modelo.
O guardrail IA passou a abster no limiar preventivo de unidades de orçamento;
ele não calcula custo real nem substitui alerta de orçamento cloud/modelo.
O ensaio Compose efêmero executou a migration pelo owner, abriu o runtime sem
superuser/BYPASSRLS e comprovou que o tenant A não esgota a quota do tenant B;
containers, rede e volumes temporários foram removidos ao final.

**Próximos itens:** homologar OIDC/MFA/RBAC real, Teams/e-mail e escalonamento;
homologar inventário Azure/AWS e recuperar collectors Linux/Docker/Windows/rede/APM
e Kubernetes/bancos sob acesso TQI autorizado;
conectar provider de IA somente atrás do guardrail/auditoria e ampliar MCP após
cada API ter isolamento, freshness e limites próprios.

**Bloqueios externos:** identidade administrativa/Run Command para VMs Azure,
VPN e gateway mTLS, IdP TQI e alvo de produção com janela/rollback.
