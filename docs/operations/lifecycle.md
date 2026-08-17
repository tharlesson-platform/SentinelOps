# Backup, restore e upgrade

## Backup

PostgreSQL deve usar PITR e snapshot diário. Object storage requer versionamento
e replicação conforme RPO. Exporte dashboards customizados, políticas, cenários,
SLOs e runbooks como código. Registre checksum, horário, tenant e retention.

## Restore test

1. Abra ambiente isolado sem egress para integrações reais.
2. Restaure PostgreSQL no ponto selecionado e object storage em bucket novo.
3. Valide migrations, contagens por tenant e checksums de amostra.
4. Suba API/worker sem schedules; execute smoke e teste de isolamento.
5. Reative schedules somente após aprovação. Registre RPO/RTO observado.

## Upgrade

Leia release notes e breaking changes. Tire backup, renderize Helm, execute
migration test com cópia sanitizada, faça canary da API/worker, valide filas e
telemetria, então promova. Loki 3 e Tempo 3 exigem revisão cuidadosa de config;
nunca copie a configuração local para o perfil distribuído.

## Rollback

Reverter binário não reverte migration destrutiva. Migrations devem ser
compatíveis N/N-1; em incompatibilidade use roll-forward ou restore aprovado.

