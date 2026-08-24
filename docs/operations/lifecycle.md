# Backup, restore, upgrade e rollback

## Perfil Linux single-node

Crie uma passphrase aleatória, guarde-a fora do diretório de backup e aplique
modo 0600. O backup pausa temporariamente API, worker, agente e Temporal, exporta
os bancos `sentinel`, `temporal` e `temporal_visibility`, espelha o bucket e
gera checksums. O pacote final usa `age` com scrypt e autenticação; arquivo
adulterado ou truncado é recusado antes de qualquer extração.

```bash
mkdir -p .sentinelops/secrets
openssl rand -base64 -out .sentinelops/secrets/backup-passphrase 48
chmod 600 .sentinelops/secrets/backup-passphrase
./scripts/backup-local.sh \
  --passphrase-file .sentinelops/secrets/backup-passphrase \
  --project-name sentinelops
```

Um backup não conta para DR até ser restaurado. O restore só aceita um projeto
novo terminado em `-restore`, remove binds de porta, autentica o pacote, valida
checksums e deixa PostgreSQL/MinIO isolados para QA:

```bash
./scripts/restore-local.sh \
  --archive artifacts/backups/sentinelops-backup-AAAAMMDDTHHMMSSZ.tar.gz.age \
  --passphrase-file .sentinelops/secrets/backup-passphrase \
  --target-project sentinelops-dr-restore \
  --confirm RESTORE
```

Registre duração, tamanho, contagens, RPO observado e evidência de leitura por
tenant. Não remova o projeto restaurado antes da aprovação. Para desligá-lo
preservando volumes, use `docker compose -p sentinelops-dr-restore down` com o
mesmo base/override utilizado pelo runbook da mudança; remoção de volumes exige
autorização destrutiva separada.

## Upgrade transacional do single-node

```bash
./scripts/upgrade-local.sh upgrade \
  --passphrase-file .sentinelops/secrets/backup-passphrase \
  --confirm UPGRADE
```

O script retém cada imagem anterior sob tag dedicada, registra IDs imutáveis em
`artifacts/upgrades/`, cria backup, reconstrói, força a recriação dos serviços
próprios e executa `doctor`. Build, deploy ou health gate falhos acionam
rollback. Ensaie o caminho sem criar falha de aplicação:

```bash
./scripts/upgrade-local.sh upgrade \
  --passphrase-file .sentinelops/secrets/backup-passphrase \
  --confirm UPGRADE --simulate-failure
```

Rollback manual:

```bash
./scripts/upgrade-local.sh rollback \
  --manifest artifacts/upgrades/rollback-AAAAMMDDTHHMMSSZ.manifest \
  --confirm ROLLBACK
```

Reverter binário não reverte migration destrutiva. Migrations precisam ser
compatíveis N/N-1; quando não forem, use roll-forward ou restore aprovado.

## Produção Kubernetes/HA

- PostgreSQL gerenciado: PITR, backup cross-account/subscription, teste mensal
  e réplica/restore em região secundária.
- Object storage: versionamento, criptografia, Object Lock quando requerido,
  lifecycle e replicação.
- Helm/GitOps: render/diff por digest, canário de API/worker, PDB e topology
  spread; migrations compatíveis antes da promoção.
- Mimir/Loki/Tempo/Pyroscope/Temporal: backup conforme produto, quotas e
  retenção por tenant; não copie volumes do Compose.
- DR: DNS/ingress, IdP, secrets, PKI, filas e conectividade devem fazer parte do
  ensaio. Declare RTO/RPO e pare se a perda observada exceder o aprovado.

O script local é evidência do mecanismo single-node, não substitui PITR,
replicação ou um ensaio no cluster e conta cloud reais.
