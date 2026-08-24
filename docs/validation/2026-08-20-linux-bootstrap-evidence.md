# Evidência local — hardening, Linux, ingestão, APM e lifecycle

Data: 2026-08-20. Host: macOS arm64 + Docker Desktop. Escopo: prova local
reproduzível; não substitui homologação no servidor, IdP, registry ou cluster
produtivos.

## Build, testes e supply chain

- todos os pacotes Go passaram em Go 1.26.6, incluindo rejeição de backup age
  adulterado;
- ShellCheck e `sh -n` passaram nos bootstraps/lifecycle;
- Compose base, collector e perfil `secure-ingest` renderizaram;
- Helm lint e render default passaram; os perfis produtivos pequeno e
  distribuído recusaram inputs/digests ausentes, renderizaram 26 recursos com
  valores explícitos e passaram no Kubeconform 0.8.0 strict para Kubernetes
  1.35;
- Alloy `v1.18.1-sentinel.1` foi reconstruído com Go 1.26.6, go-git/x-mod
  corrigidos e patch oficial Moby AuthZ;
- Trivy 0.73.0 retornou zero HIGH/CRITICAL no OS e binário do Alloy corrigido;
  SBOM CycloneDX e digest local foram gerados em `artifacts/security/`.
- o scan final também retornou zero HIGH/CRITICAL para API, worker, migrate,
  agent, backupcrypt, CLI, demo, Web e Alloy; os workflows cobrem as nove
  imagens.
- actions de CI/release foram fixadas por commit SHA; release examina candidatos
  amd64 e arm64 antes de promover qualquer tag, assina cada digest e só conclui
  a entrega com um GitHub Release contendo o manifesto assinado das nove
  imagens; o digest multiarch publicado como candidato também é escaneado antes
  da assinatura.

O digest local do build arm64 final, executando como `473:473`, foi
`sha256:c045bc64dee1bbe8d070e54e8b5ccf6b37180b4379bae48d9d94c3cfe4c26d14`.
Ele é evidência local, não digest publicado/multiarch nem assinatura.

## Matriz Linux

`scripts/test-linux-matrix.sh` executou preflight e contrato CLI do servidor e
collector nas imagens fixadas de Ubuntu 24.04, Debian 12, Rocky 9 minimal,
Fedora 42, openSUSE Leap 15.6 e Alpine 3.23.3. A matriz detectou e corrigiu a
ausência de `hostname` no Rocky minimal. Instalação real do daemon Docker e
systemd/OpenRC ainda deve ser provada em cada host alvo.

## Ingestão, agente e tenant

- Caddyfile validado com Caddy 2.11.4 fixado por digest;
- cliente sem certificado: handshake recusado (`curl` exit 56);
- registro com mTLS: HTTP 201;
- reutilização do bootstrap token: HTTP 401;
- heartbeat com token/certificado vinculados: HTTP 200;
- fingerprint persistido e igual ao certificado: verdadeiro;
- certificado válido da CA, mas SAN de outra organização: HTTP 401;
- Alloy reportou todos os componentes saudáveis e preservou `tenant_id` até
  `X-Scope-OrgID` nos exporters.

## Banco e isolamento multi-tenant

- migração habilitou e forçou RLS em 41 tabelas tenant-aware;
- role de runtime comprovada com `rolsuper=false` e `rolbypassrls=false`;
- contexto de tenant vazio não retornou registros e a organização A não leu a
  organização B;
- inserts cross-tenant diretos e por tabelas filhas foram recusados;
- a role de runtime não conseguiu forjar o contexto interno de migração `*`;
- bootstrap de roles e migrador são idempotentes e separados do ciclo de vida
  da API/worker.

## Kubernetes, GitOps e data plane

- gateway Caddy HA terminou TLS/mTLS e validou o Caddyfile produtivo completo;
- API, worker e migrador usam trust bundle explícito, e apenas o migrador recebe
  a URL do proprietário do schema;
- ExternalSecret cobre URLs de runtime/migração, JWT, proxy, webhooks, TLS do
  servidor, CA cliente e CA de upstream;
- renderer Argo CD recusou valores sem production gate e aceitou somente SHA Git
  exato e arquivo de values interno ao chart;
- raízes Terraform DEV/STG/PRD passaram em `fmt`, `init -backend=false` e
  `validate` com Terraform 1.15.9; nenhum `plan` ou `apply` de cloud foi
  executado.

## Gates observacionais

`scripts/prove-observational-gates.sh` produziu:

```text
standard_pass_result=PASS
standard_pass_checks=5
missing_policy_result=INCONCLUSIVE
over_threshold_result=FAIL
```

Os cinco checks são sintético HTTP, PromQL, LogQL, burn-rate SLO e TraceQL.
Queries recebem `X-Scope-OrgID`; ausência de política/amostra nunca aprova.

## Collector e APM

O collector usa Unix Exporter embutido, logs com redaction e cAdvisor opt-in.
Um collector real enviou métrica de host e log pelo gateway mTLS. O bootstrap
APM foi gerado para Java, Spring, Quarkus, Node, NestJS, Python, FastAPI,
Django, .NET, Go e React. A prova mTLS correlacionada resultou em:

```text
tenant_metric_samples=1
tenant_log_records=1
tenant_trace_matches=1
```

## Backup, restore, upgrade e rollback

- backup final `.tar.gz.age` modo 0600, três dumps PostgreSQL, bucket,
  metadata e checksums;
- restore autenticado em `sentinelops-final-restore`: todos checksums OK;
- contagens restauradas: `organizations=1`, `services=1`, `releases=12`;
- upgrade reteve cinco imagens anteriores, fez backup/rebuild/recreate;
- falha pós-deploy simulada acionou rollback, recriou os serviços e `doctor`
  passou.

## Limites externos restantes

- publicar imagens amd64/arm64 em registry por digest, assinar e comprovar
  admission policy;
- checks remotos terminais no SHA publicado;
- OIDC/MFA/grupos no IdP real e role bindings provisionados;
- Mimir/Loki/Tempo/PostgreSQL/Temporal HA e multi-tenant no cluster alvo;
- restore/DR regional e RTO/RPO aprovados;
- cAdvisor continua exceção privilegiada opt-in.
