# SPEC-006 — Linux, processos, serviços e Docker

**Fase:** E2 · **Estado:** IN_PROGRESS · **Responsável:** SRE/Plataforma

## Problema, escopo e contratos

Coletores Linux devem observar host, processos, systemd, logs, disco, inodes,
rede e containers sem reiniciar a carga de negócio. Alloy publica métricas,
logs e OTLP por gateway mTLS; identidade, ambiente, time, site e asset ID são
derivados da instalação, nunca do payload da aplicação.

Logs Docker usam `DockerRootDir/containers` read-only. cAdvisor é opção
separada e privilegiada; nenhuma instalação depende dele. WAL, fila, limites
de disco, atrasos e descartes precisam ser expostos como métricas.

## Dados, UX, autorização e operação

O inventário usa ID estável e o catálogo separa stale de healthy. O coletor
tem certificado mínimo por host e só faz conexões de saída. Upgrade preserva
positions/WAL; rollback restaura o bundle anterior sem apagar volumes.

## Critérios e evidência

- **AC-0601:** Dado serviço de homologação parado, quando coleta ocorre, então
  métrica, log, alerta e owner são correlacionáveis.
- **AC-0602:** Dado DockerRootDir não padrão, quando instala logs, então usa o
  diretório descoberto ou falha antes de iniciar.
- **AC-0603:** Dada interrupção de gateway, quando ele retorna, então replay,
  atraso e perdas são mensurados.
- **AC-0604:** Dado cAdvisor não aprovado, quando instala perfil de logs, então
  não monta socket Docker nem executa container privilegiado.

Teste local valida Compose, configurações Alloy e instalação em matriz Linux.
Teste em alvo exige host autorizado, falha controlada, overhead e evidência de
ingestão. Rollout é host não crítico → onda por site; rollback para o bundle
anterior e revogação da credencial se necessário.
