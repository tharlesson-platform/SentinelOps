# Entrada HTTPS interna unificada

Destino DNS interno: `sentinelops.tqi.com.br A 10.10.1.20` no DNS corporativo.
Não publicar esse nome no edge público e não abrir portas novas na Internet.

## Certificado

`renew-certificate.sh` roda no próprio SentinelOps e usa a identidade técnica
`tqi-sentinelops-acme`, autorizada pelo usuário. RBAC: Reader na zona
`tqi.com.br` e DNS Zone Contributor somente no TXT
`_acme-challenge.sentinelops`. Não pode alterar registros A nem outros TXT.
O desafio DNS-01
não depende do registro A nem abre HTTP público. O certificado é confiável
pelos navegadores; o nome constará dos registros públicos Certificate
Transparency, embora o acesso aos serviços continue privado.

Instalar o script como `/usr/local/sbin/renew-sentinelops-certificate`, modo
0750, e as unidades em `/etc/systemd/system/`. Executar a unidade uma vez e
habilitar o timer. Chaves e estado ACME ficam fora do Git, em diretórios 0700,
sob `/opt/sentinelops/private-ingress/`. A credencial Azure fica em
`secrets/azure-client-secret`, modo 0600, montada como arquivo no lego; IDs e
parâmetros não secretos ficam em `secrets/azure-dns.env`. Nunca copiar esses arquivos para
evidências. A verificação diária renova somente dentro de 30 dias do vencimento.
Rotacionar a credencial Azure antes de `2027-09-16T14:24:48Z`.

## Caminhos e gate de ativação

| Caminho planejado | Serviço real | Tipo |
|---|---|---|
| `/sentinelops/` | Build web com base nativa; API HA em loopback 8080 | Interface autenticada |
| `/grafana/` | Grafana, porta 3001 | Dashboards, Explore e APM |
| `/prometheus/` | Prometheus, porta 9090 | Métricas e consultas |
| `/temporal/` | Temporal UI, porta 8088 | Workflows |
| `/pyroscope/` | Grafana Explore, fonte Pyroscope | Perfis contínuos; ingestão 4040 intocada |
| `/keycloak/` | Keycloak, porta 8081 | Identidade; dados preservados em bind persistente |
| `/loki/` | Loki, porta 3100 | API de leitura; não possui UI própria |
| `/tempo/` | Tempo, porta 3200 | API de leitura; traces via Grafana |

Não publicar PostgreSQL, MinIO/S3, Temporal gRPC, coletores ou dados demo.
Ingestão OTLP/mTLS existente não muda. APIs sem autenticação própria exigem
proteção adicional e bloqueio dos métodos de escrita antes da publicação.

**Restrição:** Nginx escuta somente `10.10.1.20:80/443`, com filtro pelo IP
real do peer antes dos redirects e handlers: redes corporativas `10.10.0.0/16`,
VPN `10.212.0.0/16` e hosts internos `172.48.10.0/24`. A rede do edge público
`172.48.30.0/24` está excluída. X-Forwarded-For recebido não altera a decisão.
Não existe rota deste nome no balanceador Azure. Diagnóstico HTTPS somente
em loopback `127.0.0.1:19443`. A configuração e os certificados são montados
por diretório, evitando arquivos antigos após substituição atômica.
Não editar/remover as rotas atuais de Helpdesk e aplicações de negócio.

`backend-subpaths.yml` configura as UIs nativamente. Grafana usa root_url com
`/grafana/`, com strip no proxy; Prometheus usa external-url e route-prefix
interno `/`, preservando clientes internos; Temporal UI usa public path.
SentinelOps usa base `/sentinelops/` no build, API e callbacks OIDC. Antes da
mudança, Keycloak tinha somente realm master e clientes padrão, sem clientes
das aplicações; SentinelOps usa AUTH_MODE=local. Os dados Keycloak foram
copiados com o container parado para backup e bind persistente antes do novo
caminho. Não substituir dados
ausentes por exemplos, mock ou estado "OK".

## Aceitação obrigatória

1. Backup dos arquivos não secretos do Traefik e Compose; preservar rollback.
2. Validar certificado, cadeia, SAN, validade e timer.
3. Ajustar e validar cada subpath com sessão administrativa no SentinelOps.
4. Testar HTML, assets, redirects, login e sessão real em cada UI.
5. Testar dados reais e bloqueio de escrita nas APIs publicadas.
6. Ativar router privado e testar acesso VPN e negação pelo edge público.
7. Validar novamente Helpdesk e as rotas pré-existentes.

Prometheus e Temporal exigem sessão Grafana e somente GET nesta entrada.
Cookies Grafana usam Path=/ e
Secure/HttpOnly. Loki e Tempo têm somente APIs GET publicadas, também com
autenticação Grafana; seus caminhos raiz abrem Explore. Pyroscope publica
somente o serviço de consultas, sem endpoints de ingestão. Keycloak e
SentinelOps mantêm autenticação própria. APIs internas originais não mudam.

## Instalação e persistência

Instalar nginx.conf em `/opt/sentinelops/private-ingress/config/nginx.conf`,
Compose/overlay/portal na raiz da mesma pasta e o build Vite em
`web/sentinelops/`. Instalar as três unidades fornecidas em systemd e habilitar
`sentinelops-private-ingress.service` e `sentinelops-certificate.timer`.
A unidade re-aplica o overlay dos quatro backends depois da unidade principal
no boot, sem recriar API/worker/coletores. Para aplicar configuração do gateway,
validar `docker exec sentinelops-private-ingress nginx -t -c
/etc/nginx/private/nginx.conf` e fazer reload com o mesmo `-c`.

Executar `sudo python3 /opt/sentinelops/private-ingress/validate-ingress.py`.
O teste usa credenciais locais apenas em memória, sem imprimi-las, e não gera
telemetria falsa. Retorno zero significa gates HTTP/login/assets/API satisfeitos,
não cobertura completa de APM nem saúde das aplicações.

Rollback: parar a unidade de ingress e reaplicar os Compose originais sem
`backend-subpaths.yml`. Manter o bind de dados Keycloak para não perder o realm;
retirar somente as configurações de hostname/subpath. Backups ficam em
`/opt/sentinelops/private-ingress/backups/`, acesso root.

## Evidência observada em 16/09/2026

- Emissão inicial preparada no tqi-platform; emissão independente no próprio
  SentinelOps concluída com a nova identidade. SAN `sentinelops.tqi.com.br`,
  emissor Let's Encrypt YE2, vencimento `2026-12-15T13:37:27Z`.
- Cadeia validada por `openssl verify` contra `/etc/ssl/certs`: OK.
- Emissão e segunda execução manual da unidade: `Result=success`,
  `ExecMainStatus=0`. Timer ativo, próxima verificação observada em
  `2026-09-17T03:20:39Z` (horário do servidor UTC, com atraso aleatório).
- Diretórios ACME/certs 0700 root; chave 0600 root.
- Upstreams HTTP acessíveis pelo balanceador nas portas 3000, 3001, 8088,
  9090, 3100, 3200, 4040 e 8081. Isso não comprova login, assets nem dados.
- Traefik permaneceu em execução; endpoint HTTPS `/sso/login.action` do
  Helpdesk retornou 200 com verificação TLS habilitada.
- Entrada HTTPS ativa no próprio `10.10.1.20`; nenhuma porta pública Azure
  nem rota SentinelOps no Traefik do tqi-platform criada.
- Acesso SSH e sudo fornecidos pelo usuário via sessão autenticada, sem
  instalar chaves SSH novas nem alterar permissões de acesso administrativo.
