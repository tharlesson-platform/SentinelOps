# Backup, restore, upgrade e rollback

## Solicitações de exportação e exclusão

O control plane registra solicitações de ciclo de vida pela API, sempre
tenant-scoped e somente para `Platform Administrator`. A criação exige
`requestType` (`export` ou `erasure`), motivo, `subjectRef` pseudônimo e pelo
menos um domínio permitido (`control-plane-metadata`,
`operational-evidence` ou `telemetry-references`). Não inclua conteúdo pessoal
bruto na referência ou no motivo.

Uma pessoa diferente deve aprovar a solicitação; o banco impõe
`requested_by <> approved_by`, a transição é atômica e ambos os passos gravam
audit event no mesmo commit. `approved` **não** inicia exportação, retenção ou
exclusão automaticamente. Isso evita uma exclusão acidental sem contrato por
backend, política de retenção, revisão jurídica e evidência de escopo.

Há uma exceção estritamente delimitada: uma solicitação `erasure` aprovada com
domínio **exclusivo** `control-plane-metadata` pode receber a confirmação
literal `ERASE_CONTROL_PLANE_METADATA`. O executor redige `email` e
`display_name`, desabilita e substitui o subject do perfil local por referência
irreversível, e remove os `role_bindings` ativos. Ele registra contagens sem o
subject na evidência e não pode ser executado duas vezes. Auditoria histórica,
exportação, telemetria, objetos e `operational-evidence`/`telemetry-references`
continuam fora deste contrato e são recusados, não ignorados.

Antes de conectar um executor, aprove em mudança explícita: owner do dado,
fontes e destinos, prazo de retenção, criptografia/segregação do pacote de
exportação, validação do escopo, rollback quando aplicável e evidência de
conclusão. O executor deverá atualizar a solicitação somente depois de provar
o resultado; até então `requested`/`approved` são registros de governança, não
comprovantes de atendimento.

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
novo terminado em `-restore` **sem containers ou volumes Compose preexistentes**,
remove binds de porta, autentica o pacote, rejeita paths perigosos, valida o
contrato v1/checksums e deixa PostgreSQL/MinIO isolados para QA:

```bash
./scripts/restore-local.sh \
  --archive artifacts/backups/sentinelops-backup-AAAAMMDDTHHMMSSZ.tar.gz.age \
  --passphrase-file .sentinelops/secrets/backup-passphrase \
  --target-project sentinelops-dr-restore \
  --confirm RESTORE
```

Ao terminar, o script grava `artifacts/evidence/dr-restore-*/metadata.json` e
`row-counts.txt`, contendo hash do archive, duração, metadados não secretos e
contagens. Registre também tamanho, RPO observado e evidência de leitura por
tenant. Esse arquivo só prova o restore Compose local; não prova RTO/RPO
aprovado, PITR, recuperação de CA/segredos nem DR de produção. Não remova o projeto restaurado antes da aprovação. Para desligá-lo
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
