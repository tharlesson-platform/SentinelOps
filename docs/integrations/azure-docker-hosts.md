# Observabilidade de hosts Docker Azure

## Escopo inicial

| Host | Papel | Onda |
|---|---|---:|
| `tqi-platform` | aplicações Docker privadas | 1 |
| `easy-vm` | aplicações Docker privadas do Easy | 1 |
| `gitlab-vm` | GitLab Omnibus e Runner em Docker | 2 |
| `tqi-platform-edge` | entrada de rede sem Docker; collector Linux separado | fora do escopo Docker |

Os três hosts Docker privados recebem o mesmo baseline de inventário, métricas
e logs. A classificação foi revalidada em 2026-09-14: o edge não possui Docker
e não deve ser contado como cobertura Docker. Seu onboarding Linux depende de
rota ao gateway e possui gate próprio.

## Descoberta sem impacto

Em cada host, execute `sudo ./scripts/discover-docker-host.sh`. O artefato não
coleta variáveis de ambiente, labels, conteúdo de logs ou segredos. Ele registra
versão e diretório do Docker, containers, imagens, redes, listeners e capacidade
de disco. Anexe o `inventory.json` sanitizado à mudança de onboarding.

## Conectividade obrigatória

O collector inicia somente conexões de saída para o gateway mTLS do SentinelOps.
Antes da instalação, é obrigatório comprovar uma rota privada (VPN, peering ou
ExpressRoute) entre o host Azure e o gateway. Se a rota privada não existir, a
mudança deve parar: não exponha Prometheus, Loki, Tempo ou OTLP diretamente na
Internet. O firewall deve permitir apenas o destino do gateway e a porta HTTPS
de ingestão; o certificado por host limita a identidade do collector.

## Host Docker

Gere um bundle com `--with-containers`. Além das métricas de host, esse perfil
coleta os logs JSON dos containers por mount somente leitura em
`DockerRootDir/containers`, sem montar `docker.sock`. Se o runtime usa um
caminho não convencional, passe `--docker-log-root` na configuração após
conferir o diretório no host.

Use `--with-cadvisor` somente depois de aprovar a exceção privilegiada do
host; ele não é ativado pelo perfil de logs.

Os logs são interpretados como JSON Docker, recebem timestamp e label de stream,
e linhas que aparentem conter credenciais são descartadas. Não adicione IDs de
requisição, usuário, URL ou container como labels de Loki: mantenha-os no corpo
estruturado para evitar alta cardinalidade.

## Edge sem Docker

O edge usa o baseline Linux sem os perfis `containers` e `cadvisor`. Não
instale componentes Docker apenas para hospedar o collector. A rota privada
para `sentinelops:8443/44317` é pré-requisito; ausência de rota mantém o host
`BLOCKED`, mesmo que a VM esteja `running` no Azure.

## Evidência atual

O estado por host, mudanças executadas e rollback estão em
[`azure-docker-rollout-2026-09-14.md`](../production-readiness/azure-docker-rollout-2026-09-14.md).
