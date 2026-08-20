# Evidência — bootstrap Linux, collector e onboarding APM

Data: 2026-08-20. Escopo: validação local e containers Linux arm64; não é
aprovação de produção nem substitui teste em servidor/distribuição alvo.

## Estrutura validada

- scripts POSIX passaram em `sh -n`;
- help/argumentos dos três bootstraps foram executados;
- Compose central e Compose do collector passaram em `config --quiet`;
- Alloy 1.18.1 validou `deploy/agents/linux/config.alloy`;
- Promtool 3.13.2 validou seis regras;
- preflight do servidor e do collector passou dentro de Alpine 3.23.3 arm64;
- kits APM foram gerados para Java, Spring, Quarkus, Node, NestJS, Python,
  FastAPI, Django, .NET, Go e React; os JSONs foram validados por `jq` e os
  arquivos de ambiente permaneceram com modo 0600.
- o primeiro `make test` atual revelou que a imagem Go Alpine não possuía CGO
  para `-race`; a suíte foi separada para `golang:1.26.6-bookworm`.
- após a correção, `make test` passou com Go race detector, Vitest e build Vite;
- `make lint`, `go vet`, ShellCheck, `doctor` e `prove-gates.sh` passaram;
- Trivy filesystem terminou com zero HIGH/CRITICAL em dependências do projeto,
  zero secrets e zero misconfigurations reconhecidas;
- o Prometheus recarregou e expôs o grupo `sentinelops-linux-hosts` com as novas
  regras de filesystem, memória e clock skew.

## Prova dinâmica do collector

O collector foi executado com Alloy 1.18.1 e Unix Exporter embutido. O primeiro
start revelou e corrigiu dois problemas reais antes da entrega:

1. mount propagation `rslave` incompatível fora de Linux nativo;
2. inicialização do volume Alloy incompatível com `cap_drop: ALL`.

A solução final usa mount raiz somente leitura e um init container com apenas
`CAP_CHOWN`; o processo principal executa como UID/GID 473 sem capabilities.

As imagens oficiais externas Node Exporter 1.12.1 e cAdvisor 0.60.5 foram
retiradas do caminho após o Trivy encontrar oito vulnerabilidades HIGH
corrigíveis em cada binário. O perfil final usa os componentes embutidos no
Alloy; cAdvisor permanece opt-in e privilegiado.

O scan do Alloy 1.18.1 encontrou doze vulnerabilidades HIGH corrigíveis no
binário, incluindo stdlib Go e dependências Docker/go-git/x/mod. Em 2026-08-20,
1.18.1 continuava sendo a release oficial mais recente. Portanto, este
collector permanece restrito a laboratório/piloto até uma imagem upstream
corrigida ou um build interno revisado e assinado.

Com o collector pronto:

- Prometheus recebeu `up{job="linux-node",instance="validation-host"}`;
- Prometheus recebeu `node_uname_info{instance="validation-host"}`;
- um log OTLP enviado ao collector foi localizado no Loki com
  `service_name="collector-smoke"`;
- um span de erro OTLP atravessou collector → Alloy central → Tempo e foi
  consultado pelo trace ID.

## Limites

- o preflight foi executado em container Alpine; ainda é necessário executar
  o instalador em cada distro/servidor alvo;
- cAdvisor permanece opt-in por exigir privilégio e mounts sensíveis;
- endpoints HTTP remotos são aceitos somente com `--allow-insecure` explícito;
- gateway TLS/mTLS, OIDC corporativo, HA, DR e isolamento multi-tenant continuam
  bloqueadores produtivos descritos em `docs/operations/documentation-status.md`.
- a imagem Alloy atual possui findings HIGH corrigíveis e bloqueia PRD.
