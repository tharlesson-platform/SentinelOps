# SPEC-007 — Windows, AD e serviços Microsoft

**Fase:** E3 · **Estado:** IN_PROGRESS · **Responsável:** SRE/Infraestrutura

## Problema, escopo e contratos

Windows requer instalador silencioso versionado, serviço recuperável, Alloy ou
exporter com permissões mínimas e bookmarks de eventos. Cobrir CPU, memória,
disco, rede, serviços, System/Application/Security e packs IIS, .NET, SQL,
DNS, DHCP e AD conforme inventário aprovado. Não coletar senha ou conteúdo de
segurança fora da política.

## Dados, UX, autorização e operação

Cada host usa identidade mTLS própria, asset ID estável e escopo de site.
Eventos carregam provenance e checkpoint; falta de permissão vira erro
observável, nunca healthy. Upgrade/rollback devem preservar a configuração
anterior e o bookmark.

## Critérios e evidência

- **AC-0701:** Dado serviço de homologação parado, quando o evento é emitido,
  então sinal, alerta e runbook aparecem com host e horário.
- **AC-0702:** Dado restart do coletor, quando retoma, então bookmark evita
  duplicação/perda silenciosa.
- **AC-0703:** Dado usuário sem permissão, quando acessa canal protegido, então
  a falha é visível e não há coleta parcial aprovada.
- **AC-0704:** Dada jornada AD, quando bind/porta estão saudáveis mas login
  falha, então o sintético não aprova autenticação.

O primeiro corte local inclui instalador PowerShell com SHA-256 obrigatório,
serviço Alloy sob `LocalService`, ACLs restritas, Application/System com
bookmarks persistentes e Security opt-in com mensagem/dados de evento
suprimidos. Os packs AD/IIS/SQL/DNS/DHCP só entram após verificar o role local.
O contrato estático é coberto por `scripts/harness/check_windows_collector.sh`;
execução PowerShell e todos os critérios ainda dependem de host Windows e
política AD autorizados. Rollout usa OU/piloto não crítico; rollback restaura
configuração e bookmark sem apagar evidência; rollback binário requer o
instalador anterior verificado na janela de mudança.
