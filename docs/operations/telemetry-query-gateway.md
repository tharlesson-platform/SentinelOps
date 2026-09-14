# Gateway confiável de consulta de telemetria

## Finalidade

Prometheus, Loki e Tempo não devem receber consultas diretamente do worker no
perfil produtivo. O worker de validação recebe um certificado mTLS com SAN
`spiffe://sentinelops/workloads/release-validator` e consulta somente o
listener interno `:8444` do gateway.

As únicas rotas permitidas são:

| Rota do worker | Upstream | Rota encaminhada |
|---|---|---|
| `/prometheus/api/v1/query_range` | Prometheus/Mimir | `/api/v1/query_range` |
| `/loki/loki/api/v1/query_range` | Loki | `/loki/api/v1/query_range` |
| `/tempo/api/search` | Tempo | `/api/search` |

O workflow obtém a organização do registro tenant-scoped no PostgreSQL. O
worker envia esse contexto ao gateway autenticado por mTLS; o gateway remove
o cabeçalho de entrada e injeta `X-Scope-OrgID` apenas no upstream. Nenhuma
rota aceita URL, host ou caminho arbitrário.

## Instalação e rotação

1. Emita um certificado exclusivo para o worker com o SAN acima, assinado pela
   CA configurada em `gateway.clientCASecretName`.
2. Crie o Secret indicado por `global.telemetryQueryClientTLSSecretName` com
   as chaves configuradas em `telemetryQueryClientTLSCertKey` e
   `telemetryQueryClientTLSKeyKey`.
3. Configure os backends para exigir e respeitar o tenant injetado, e restrinja
   sua rede para aceitar consultas somente do gateway.
4. Aplique o render, confirme que o worker não possui egress para 9090, 3100
   ou 3200 e execute uma validação de release em tenant de homologação.
5. Para rotação, publique certificado novo, reinicie gradualmente os workers,
   confirme consultas e revogue o anterior na CA.

O processo falha na inicialização produtiva se URL HTTPS ou os caminhos do
certificado/chave estiverem ausentes. O Compose de desenvolvimento mantém
acesso direto por ser laboratório; não é evidência de isolamento.

## Aceite no alvo e rollback

No alvo autorizado, prove uma consulta por organização, uma tentativa com SAN
inadequado e uma tentativa de acesso direto ao backend. Guarde IDs de query e
timestamps sanitizados. Se a rotação falhar, restaure somente o certificado
anterior ainda válido e reverta o deployment; não abra as portas diretas do
worker como atalho.
