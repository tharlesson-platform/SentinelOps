# Runbook de entrega e ativação

## Objetivo

Entregar uma versão imutável, instalar em fases, provar o caminho funcional e
preservar rollback. Um processo concluído não equivale a aprovação produtiva:
os gates de segurança, HA, tenant e DR permanecem obrigatórios.

## 1. Preparação da entrega

- confirme commit/tag e árvore Git limpa;
- gere pacote, SHA-256, SBOM e relatório de vulnerabilidade;
- registre as imagens por digest, não somente tag;
- liste novas variáveis e migrations;
- identifique operador, ambiente, janela, comunicação e rollback owner;
- confirme backup e restore test compatíveis com o RPO/RTO.

## 2. Pré-deploy no servidor

```bash
./scripts/install-linux-server.sh --phase preflight
./scripts/install-linux-server.sh --phase runtime
docker compose --env-file .env -f deploy/compose/docker-compose.yml config --quiet
```

Registre espaço, memória, portas, versão Docker/Compose e estado anterior.

## 3. Configuração

```bash
./scripts/install-linux-server.sh --phase configure
stat -c '%a %n' .env
```

O resultado esperado para `.env` é `600`. Compare chaves, não valores, entre a
versão anterior e a nova. Nunca inclua secrets no log da mudança.
Confirme também `.sentinelops/secrets/pki-ca-passphrase` em 0600, validade/SAN
do certificado do gateway, presença de `MTLS_PROXY_SHARED_SECRET` e owners das
rotações. Compare apenas presença/comprimento; nunca imprima o valor.

## 4. Deploy

```bash
./scripts/install-linux-server.sh --phase deploy
docker compose --env-file .env -f deploy/compose/docker-compose.yml --profile secure-ingest ps
```

Interrompa se houver restart loop, migration incompatível, volume inesperado,
porta pública não planejada ou health check degradado.

## 5. Configuração inicial

```bash
./scripts/install-linux-server.sh --phase seed
./scripts/install-linux-server.sh --phase seed
```

A segunda execução deve ser idempotente. Em ambiente sem demo, use
`--without-seed` no fluxo completo e cadastre serviços reais após a validação.

## 6. Aceite pós-deploy

```bash
./scripts/install-linux-server.sh --phase verify
./scripts/prove-gates.sh
./scripts/prove-observational-gates.sh
```

Registre:

- saída do `doctor` e estado dos containers;
- release/commit/digest instalados;
- autenticação e carregamento da UI;
- uma release saudável `PASS` e uma falha controlada `FAIL`;
- política observacional ausente `INCONCLUSIVE` e cinco checks `PASS` quando configurados;
- mTLS sem certificado recusado, bootstrap one-time não reutilizável, headers
  de proxy forjados recusados e tenant/nome/fingerprint vinculados;
- queries de Prometheus, Loki, Tempo e Pyroscope;
- logs sem secrets;
- horário UTC e BRT.

## 7. Handoff

Entregue ao time operador:

- URL e método de acesso;
- secret manager/owner das credenciais, sem colar os valores;
- inventário de portas e regras de firewall;
- comandos de start/stop/log/doctor;
- política de backup, restore, retenção e capacidade;
- runbooks e escalonamento;
- lista explícita de limitações e bloqueadores produtivos.

## 8. Rollback

No single-node, prefira o mecanismo ensaiado:

```bash
./scripts/upgrade-local.sh rollback --manifest MANIFEST --confirm ROLLBACK
```

Se não houve migration incompatível:

1. preserve logs e artefatos da falha;
2. volte ao pacote/commit e digests anteriores;
3. execute a fase `deploy`;
4. execute `verify` e uma jornada funcional;
5. confirme que filas e dados permanecem consistentes.

Se houve migration incompatível, não execute rollback de imagem isoladamente.
Use o plano de roll-forward ou restore aprovado e registre perda/RPO observado.

## 9. Encerramento

A mudança só termina com evidência funcional, observabilidade atual, rollback
preservado e pendências atribuídas. `docker compose up`, containers `running`
ou uma tela acessível isoladamente não satisfazem o aceite.
