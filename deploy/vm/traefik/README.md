# Traefik interno para o perfil single-node

Este edge expõe exclusivamente as interfaces humanas que já estão em loopback
no SentinelOps. Ele usa rede do host para alcançar os upstreams locais e não
publica dashboard próprio, PostgreSQL, Temporal gRPC, Prometheus, Loki, Tempo,
Pyroscope, MinIO, Alloy ou API direta.

| Entrada | Serviço |
|---|---|
| TCP 80 | SentinelOps Web |
| TCP 3001 | Grafana |
| TCP 8088 | Temporal UI |

O gateway mTLS do SentinelOps continua separado em TCP 8443 (HTTP) e 44317
(gRPC), com certificado de cliente obrigatório. Em ambiente corporativo, limite
essas cinco portas aos CIDRs administrativos e de coletores. Para HTTPS das
UIs, substitua o entrypoint HTTP por certificado emitido pela PKI corporativa e
nome DNS interno; não use certificado autoassinado como solução permanente.
