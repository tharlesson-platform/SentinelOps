# Produção

O caminho executável de servidor Linux está em
[instalação Linux](../installation/linux-server.md). Ele é deliberadamente
classificado como single-node/laboratório/piloto. Este documento define o gate
adicional para produção Kubernetes/HA.

## Gate de entrada

Não promova enquanto OIDC/MFA e role bindings não estiverem homologados no IdP,
imagens próprias não estiverem publicadas/assinadas por digest e backup/restore
não tiver evidência no ambiente-alvo. O chart falha o render produtivo quando
faltam digest, issuer/origin HTTPS, allowlist SSRF, gateway mTLS, ingress CIDR
ou endpoints observacionais HTTPS.

## Perfis

- Small: API/Web/Worker >=2 réplicas, Grafana HA, Prometheus ou Mimir compacto,
  Loki monolítico HA, Tempo/Pyroscope, Temporal e PostgreSQL protegidos.
- Distributed: filas independentes, Mimir e Loki microservices, Tempo/Pyroscope
  escaláveis, query frontends, object storage, PostgreSQL/Temporal HA,
  anti-affinity, topology spread e DR.

## Bootstrap produtivo faseado

1. **Identidade e alvo:** confirme conta, região, cluster, DNS, CNI, ingress/LB,
   IdP e owners; declare RTO/RPO e janela.
2. **Data plane:** preencha o root DEV/STG/PRD, execute `init/validate/plan`,
   obtenha aprovação e somente então `apply`; habilite Temporal e backends
   observacionais HA/multi-tenant fora deste chart.
3. **Banco:** execute `bootstrap-postgres-roles.sh`; prove que runtime não é
   superuser/BYPASSRLS e guarde separadamente URLs runtime/migração.
4. **Secrets e PKI:** instale External Secrets/SecretStore, publique URLs,
   webhook map, segredo gateway, trust bundle, certificado servidor e CA dos
   agentes. Teste emissão, expiração, revogação e rotação.
5. **Supply chain:** faça merge no SHA revisado, crie tag SemVer, aprove o
   Environment `production` e espere a GitHub Release com manifesto assinado
   das nove imagens.
6. **GitOps:** gere values com os digests, renderize Helm e Application presa ao
   SHA, valide schema/Caddy, revise diff e sincronize. O PreSync faz a migração
   antes de API/worker.
7. **Onboarding:** emita bootstrap token one-time e certificado por collector;
   aplique os kits APM por linguagem sem colocar secrets no repositório.
8. **Aceite:** prove tenant/RBAC negativos, mTLS, SSRF, métrica/log/trace/profile,
   gates PASS/FAIL/INCONCLUSIVE, backup/restore, upgrade/rollback e DR.

Interrompa a fase atual se seu gate falhar; não avance apenas porque Pods estão
`Running`.

Agentes não são implantados junto do control plane: devem ficar próximos aos
alvos e usar token one-time + certificado mTLS próprio. `agent.enabled=true` é
recusado pelo chart para impedir um executor central sem identidade de alvo.
Os perfis produtivos expõem o gateway por Service `LoadBalancer`, preservam o
IP de origem (`externalTrafficPolicy: Local`) e reaproveitam
`gatewayIngressCIDRs` como `loadBalancerSourceRanges`. Em clusters sem
LoadBalancer, altere para `ClusterIP` somente quando já existir um L4 externo
com TLS passthrough, health check e allowlist equivalentes.
O Secret referenciado por `global.existingSecret` precisa conter
`mtls-proxy-shared-secret` aleatório (>=32 bytes), `database-url` da role
não-superusuária, `migration-database-url` do owner, `webhook-hmac-secrets` por
organização e `jwt-secret`. A URL owner é montada somente no Job de migração;
API/worker nunca a recebem. O segredo mTLS é configurado somente no gateway; o ingress
comum nunca deve preservar headers `X-Sentinel-Client-*` enviados pelo cliente.
Consulte [isolamento PostgreSQL](database-tenancy.md).
O Secret `sentinelops-upstream-ca` precisa conter `ca-bundle.crt` com as raízes
públicas e privadas usadas por PostgreSQL, IdP e backends HTTPS. O bundle é
montado read-only e o gateway valida os certificados dos upstreams; não use
`tls_insecure_skip_verify`.

## Render produtivo mínimo

```bash
helm lint deploy/helm/sentinelops
helm template sentinelops deploy/helm/sentinelops \
  -f deploy/helm/sentinelops/values-small-production.yaml \
  --set global.oidcIssuerURL=https://id.example.net/realms/platform \
  --set global.allowedOrigin=https://sentinelops.example.net \
  --set 'global.releaseValidationAllowedHosts[0]=api.internal.example.net' \
  --set global.prometheusURL=https://prometheus.observability.svc \
  --set global.lokiURL=https://loki.observability.svc \
  --set global.tempoURL=https://tempo.observability.svc \
  --set gateway.prometheusUpstream=https://prometheus.observability.svc \
  --set gateway.lokiUpstream=https://loki.observability.svc \
  --set gateway.alloyHTTPUpstream=https://alloy-http.observability.svc \
  --set gateway.alloyGRPCUpstream=https://alloy-grpc.observability.svc \
  --set api.image.digest=sha256:... \
  --set worker.image.digest=sha256:... \
  --set web.image.digest=sha256:... \
  --set migration.image.digest=sha256:... \
  --set 'networkPolicy.databaseCIDRs[0]=CIDR_EXATO_DO_RDS' \
  --set 'networkPolicy.externalHTTPSCIDRs[0]=CIDR_EXATO_DO_IDP_E_ALVOS' \
  --set 'networkPolicy.gatewayIngressCIDRs[0]=CIDR_EXATO_DOS_AGENTES' \
  > rendered.yaml
```

Substitua seletores de namespace/pod da NetworkPolicy pelos labels reais do
cluster. CIDR amplo só para “fazer funcionar” reprova o gate. Valide a imagem
assinada em admission policy (Kyverno, Gatekeeper ou política nativa do
registry/cloud); digest sozinho garante imutabilidade, não autoria.

Gere a Application Argo somente com repositório HTTPS e commit completo:

```bash
./scripts/render-argocd-application.sh \
  --repo-url https://github.com/ORG/REPO.git \
  --revision SHA_DE_40_HEX \
  --values-file values-empresa-production.yaml \
  --output rendered/application.yaml
```

O renderer recusa branch móvel e só emite o manifest após `helm lint/template`.
O Environment GitHub `production` deve exigir reviewers; use apenas a GitHub
Release cujo manifesto de nove imagens foi verificado conforme
[processo de release](release-process.md).

## Checklist

- identidade cloud/cluster, região e tenant confirmados;
- TLS ingress e mTLS agentes, trust bundle, segredo API-gateway e rotação testados;
- OIDC/MFA, RBAC por tenant e testes negativos;
- object storage versionado/criptografado/lifecycle;
- PostgreSQL PITR, restore, pool, índices, role runtime sem superuser/BYPASSRLS
  e FORCE RLS nas 41 tabelas;
- limites de série, labels, logs, spans, queries e retention por tenant;
- image digest, SBOM, scan, assinatura e admission policy;
- PDB/HPA/resources, egress allowlist e NetworkPolicy ajustados;
- SLO e alertas do próprio SentinelOps;
- plan/diff, aprovação humana e rollback manual ensaiado.

## Evidência obrigatória

Antes da promoção, anexe ao change/release:

- digest e assinatura de todas as imagens;
- render do Helm e diff GitOps no cluster/alvo exato;
- teste negativo de tenant/RBAC;
- teste SSRF e política de egress;
- backup, restore e RTO/RPO observados;
- upgrade e rollback/roll-forward ensaiados;
- queries que comprovem métricas, logs, traces, profiles e SLO;
- checks remotos terminais no SHA promovido;
- owner, janela, comunicação e plano de interrupção.

Sem essa evidência, o status permanece laboratório/piloto mesmo que todos os
containers estejam `running`.

Antes de solicitar a promoção, execute os gates reproduzíveis:

```bash
make validate-release
./scripts/prove-remote-ci.sh
```

O primeiro cobre manifests, digests, assinatura local e sintaxe dos workflows.
O segundo requer `origin`, CI verde no SHA atual e branch protection real; ele
não converte ausência de remoto em sucesso.
