# SPEC-018 — Produção, entrega segura e DR

**Fase:** E1/E2 · **Estado:** IN_PROGRESS · **Responsável:** Plataforma/SRE

## Problema, escopo e contratos

Produção inclui control plane, data plane, IdP, TLS/mTLS, PostgreSQL, Temporal,
storage, PKI, backups, observabilidade externa e GitOps/IaC no alvo escolhido.
Imagens têm digest, SBOM, scan e assinatura; migration, upgrade, roll-forward e
rollback possuem plano e evidência.

## Dados, UX, autorização e operação

Backups são por componente e testados em restore isolado. RTO/RPO de banco não
é confundido com retenção de telemetria. Segredos/CA têm recuperação separada,
patching e acesso mínimo. Produção só aceita CI terminal do SHA/digest.

## Critérios e evidência

- **AC-1801:** Dado ambiente novo, quando aplica artefato promovido, então
  recupera serviço e dados previstos com versão rastreável.
- **AC-1802:** Dado restore, quando mede, então RTO/RPO e perdas por sinal são
  explicitamente comparados às metas.
- **AC-1803:** Dada falha física, quando ocorre, então não é confundida com
  restart de processo e alerta externo funciona.
- **AC-1804:** Dado segredo ou CA, quando recupera, então requer acesso
  separado e não aparece em logs/evidências.

O corte local agora autentica o pacote, recusa caminhos inseguros, exige alvo
Compose inédito, valida o contrato v1 e registra duração, hash do archive e
contagens restauradas sem incluir segredos. Ele é evidência de mecanismo local,
não de RTO/RPO, recuperação de CA ou DR no alvo.

Aceite exige alvo autorizado, janela, owner e rollback aprovado. Nenhum
manifest ou Compose local equivale a produção.
