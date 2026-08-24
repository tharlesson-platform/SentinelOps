# Evidência de bootstrap, hosts, aplicações e dashboards

Data: 2026-08-24.

## Escopo entregue

- bootstrap-linux.sh instala o perfil single-node desde um Linux vazio;
- download remoto exige HTTPS, SHA-256 e archive sem travessia de diretório;
- Docker/Compose, PKI, secrets, migrações, mocks e provas são faseados;
- Systemd e OpenRC são suportados;
- create-linux-collector-bundle.sh emite pacote mTLS exclusivo sem chave da CA;
- bootstrap APM cobre 11 runtimes e envia os três sinais;
- dashboards Linux, Application, APM e Docker usam consultas especializadas.

## Evidência executada

| Prova | Resultado |
|---|---|
| Testes Go com race detector | PASS |
| Teste/build frontend | PASS |
| go vet e Compose config | PASS |
| Matriz Ubuntu, Debian, Rocky, Fedora, openSUSE e Alpine | PASS |
| Instalação real de Docker 29.7.2 e Compose 5.5.0 no Rocky Minimal | PASS |
| Bundle sem ca.key ou passphrase | PASS |
| Collector SPIFFE/mTLS até série up no Prometheus | PASS |
| APM até Prometheus, Loki e Tempo | PASS |
| 35 dashboards JSON e quatro dashboards provisionados | PASS |
| Host mock e aplicações com dados vivos | PASS |
| Pipeline mock Prometheus/Loki/Tempo/Pyroscope | PASS |
| Failover com duas APIs e dois workers | PASS |
| Playwright | 1 de 1 PASS |
| k6 | 444 de 444 checks PASS; p95 3,65 ms |
| Prometheus rules | 10 regras válidas |
| Trivy Alloy e seis imagens próprias | zero HIGH/CRITICAL corrigíveis |

Artefatos principais:

- artifacts/local-e2e/20260824T152328Z.json;
- artifacts/dashboards/proof-20260824T153113Z.json;
- artifacts/onboarding-proof/sentinel-bootstrap-proof/storage-proof.json;
- artifacts/collector-bundles/evidence/srv-proof-01.json;
- artifacts/resilience/ha-20260824T152357Z.json;
- artifacts/release/controls-20260824T152727Z.json.

## Limite de produção

O ambiente local está funcional e o bootstrap é entregável. A promoção para
produção continua fail-closed enquanto não houver origin Git autorizado,
checks remotos no SHA, registry/admission e execução em cluster/contas reais.
