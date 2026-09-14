# SentinelOps nos primeiros 30 minutos

Este roteiro inicia o control plane e valida somente telemetria recebida de
hosts e aplicações reais. Integrações ausentes permanecem `Sem dados`.

## 1. Prepare o ambiente

```bash
./scripts/bootstrap.sh
make prepare-images
```

Revise `.env` localmente sem copiar credenciais para terminal compartilhado,
Git ou chat. Configure os alvos reais permitidos para synthetic e release
validation antes de iniciar workers.

## 2. Inicie a plataforma

```bash
make up
make doctor
```

O `doctor` valida API, Web, Alloy, Prometheus, Loki, Tempo e Pyroscope. Um
backend saudável não é evidência de que uma integração esteja enviando dados.

## 3. Conecte fontes reais

- Hosts Linux/Docker: use `scripts/create-linux-collector-bundle.sh`.
- Aplicações: use `make bootstrap-apm LANGUAGE=<runtime>`.
- Kubernetes, cloud, VMware e bancos: configure o coletor específico e confirme
  seu target no Prometheus antes de considerar a dashboard coberta.

Toda fonte deve carregar `deployment.environment=production`, `service.name`,
`host.name`, time responsável e identidade de tenant.

## 4. Valide os dados

```bash
make prove-dashboards
make dashboard-filters
make no-nonprod-telemetry
```

Confirme amostras reais no período atual. Painel vazio deve continuar vazio e
explicar a integração ausente; nunca substitua ausência por números estáticos.

## 5. Operação

```bash
make logs
make prove-ha
make down
```

`make down` preserva volumes. `make reset` é destrutivo e não deve ser usado em
ambientes com dados necessários.
