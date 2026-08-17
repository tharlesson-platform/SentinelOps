# Produção

## Gate de entrada

Não promova enquanto `AUTH_MODE=oidc` não estiver implementado/validado na API,
imagens não estiverem fixadas por digest, e backups/restore não tiverem evidência
no ambiente-alvo. O chart é uma base hardened, não uma autorização de deploy.

## Perfis

- Small: API/Web/Worker >=2 réplicas, Grafana HA, Prometheus ou Mimir compacto,
  Loki monolítico HA, Tempo/Pyroscope, Temporal e PostgreSQL protegidos.
- Distributed: filas independentes, Mimir e Loki microservices, Tempo/Pyroscope
  escaláveis, query frontends, object storage, PostgreSQL/Temporal HA,
  anti-affinity, topology spread e DR.

## Checklist

- identidade cloud/cluster, região e tenant confirmados;
- TLS ingress e mTLS agentes, trust bundle e rotação testados;
- OIDC/MFA, RBAC por tenant e testes negativos;
- object storage versionado/criptografado/lifecycle;
- PostgreSQL PITR, restore, pool, índices e RLS;
- limites de série, labels, logs, spans, queries e retention por tenant;
- image digest, SBOM, scan, assinatura e admission policy;
- PDB/HPA/resources, egress allowlist e NetworkPolicy ajustados;
- SLO e alertas do próprio SentinelOps;
- plan/diff, aprovação humana e rollback manual ensaiado.

