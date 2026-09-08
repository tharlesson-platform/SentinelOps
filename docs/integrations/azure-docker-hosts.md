# Observabilidade de hosts Docker Azure

## Escopo inicial

| Host | Papel | Onda |
|---|---|---:|
| `tqi-platform` | aplicações Docker privadas | 1 |
| `easy-vm` | aplicações Docker privadas do Easy | 1 |
| `tqi-platform-edge` | entrada Traefik/edge | 2 |

Os dois hosts privados recebem o mesmo baseline de inventário, métricas e logs.
O edge entra após a primeira onda provar que o collector não altera listeners,
Traefik ou rotas de aplicação.

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

Gere um bundle com `--with-containers`. Além das métricas de host e cAdvisor,
esse perfil coleta os logs JSON dos containers por mount somente leitura em
`/var/lib/docker/containers`, sem montar `docker.sock`. Se o `DockerRootDir`
descoberto for diferente, informe seu diretório `containers` em
`SENTINEL_DOCKER_LOG_ROOT` antes do deploy.

Os logs são interpretados como JSON Docker, recebem timestamp e label de stream,
e linhas que aparentem conter credenciais são descartadas. Não adicione IDs de
requisição, usuário, URL ou container como labels de Loki: mantenha-os no corpo
estruturado para evitar alta cardinalidade.

## Edge

O edge deve usar o mesmo bundle, inicialmente sem cAdvisor se a exceção
privilegiada não tiver sido aprovada. O rollout do collector não muda portas de
Traefik nem reinicia os serviços de negócio. Na fase APM, Traefik e cada serviço
receberão logs JSON, métricas e propagação W3C de trace.
