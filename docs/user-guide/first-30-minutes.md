# SentinelOps nos primeiros 30 minutos

Este roteiro leva uma pessoa sem contexto do clone até a primeira investigação
com métricas, logs, traces e profiles. Ele usa somente o ambiente local e dados
mock; não acessa produção.

## 1. Prepare o computador

Você precisa de Docker com Compose, `make`, `curl`, `jq`, `openssl` e pelo menos
8 GiB de memória livres. Clone o repositório e entre na pasta:

```bash
git clone https://github.com/tharlesson-platform/SentinelOps.git
cd SentinelOps
```

Cheque o host antes de instalar:

```bash
make doctor
```

Se o ambiente ainda não estiver iniciado, falhas de conectividade nesta etapa
são esperadas; corrija apenas pré-requisitos ausentes.

## 2. Inicie a plataforma e os mocks

```bash
make local-demo
```

O comando cria credenciais locais sem imprimi-las, constrói imagens, inicia a
plataforma, registra `storefront`, `orders` e `payments`, gera tráfego e exige
telemetria nos quatro backends. Na primeira execução, downloads podem demorar.

Recupere a senha local conscientemente e abra a interface:

```bash
make credentials
```

- SentinelOps: <http://localhost:3000>
- Grafana: <http://localhost:3001>
- Demo storefront: <http://localhost:8090/api/checkout>

## 3. Confirme a cadeia de dados

```bash
make prove-local
```

A prova termina com erro se faltar qualquer elo obrigatório. Ela grava JSON em
`artifacts/local-e2e/`, pasta ignorada pelo Git e criada com acesso restrito.
Procure no resultado o mesmo `trace_id` no Tempo e nos logs do Loki, a série no
Prometheus e o perfil no Pyroscope.

## 4. Observe uma falha controlada

```bash
curl 'http://localhost:8090/api/checkout?fault=latency&fault_service=orders'
```

No Grafana, abra a pasta `SentinelOps Managed` e use os dashboards
`Application Overview`, `Distributed Tracing`, `Logs` e `Continuous Profiling`.
A falha existe somente nos mocks.

## 5. Teste resiliência local

```bash
make prove-ha
make prove-resilience
```

A primeira prova interrompe uma réplica da API e a restaura; a segunda reinicia
o Pyroscope preservando seu volume. Isso cobre falha de processo no mesmo host,
não perda do servidor nem disaster recovery.

## 6. Encerrar ou recomeçar

```bash
make down
```

O comando preserva volumes. Para apagar apenas os volumes do projeto local:

```bash
make reset
```

`make reset` é destrutivo para os dados locais do SentinelOps. Não o execute em
um diretório ou projeto compartilhado sem confirmar o alvo.

## Próximo caminho

- Instalar um servidor vazio: [bootstrap Linux](../installation/bootstrap-zero-to-running.md).
- Coletar um host remoto: [host monitoring](../integrations/host-monitoring.md).
- Instrumentar um serviço: [APM onboarding](../integrations/apm-onboarding.md).
- Planejar produção: [production runbook](../operations/production.md).
