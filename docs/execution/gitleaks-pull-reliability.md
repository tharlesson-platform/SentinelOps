# Gitleaks CI: tolerância limitada no pull

O passo de scan continua obrigatório. `scripts/ci/gitleaks-history.sh` adquire
somente a imagem v8.30.0 pelo digest já existente; no máximo três tentativas de
`docker pull`, cada uma com GNU `timeout` de 120 s e TERM seguido de KILL após
5 s. Backoff de 5 s e 10 s: orçamento nominal máximo do pull de 390 s, além
do pequeno overhead de processos. O timeout de 60 minutos do job permanece.

As duas saídas do pull são suprimidas integralmente: erros de blob podem conter
URLs assinadas, querystrings e credenciais partidas em linhas. O log registra
apenas tentativa, resultado e exit code. Não são criados arquivos com logs crus.

Após pull bem-sucedido, há exatamente um `docker run --pull=never`, mesmo digest,
mount `$PWD:/repo:ro` e `git /repo --redact --no-banner`. O `exec` preserva o exit
code do scanner: finding (1), falha operacional (por exemplo 125) e sinais não
geram retry de scan, fallback, ignore ou sucesso artificial. Se os três pulls
falharem, retorna o último erro sem executar scanner. Códigos 130/137/143 no
pull abortam sem nova tentativa; timeout 124 pode ser repetido dentro do limite.

Dependências: Bash, Docker CLI, GNU timeout e sleep já esperados no guest Linux.
Ferramenta ausente falha com 127. Nenhuma instalação ou mudança no base guest.
Não altera Dockerfiles, triggers, labels, fetch-depth, outros scanners ou gates.

Validação local: `python3 -B scripts/ci/test_gitleaks_history.py`,
`bash -n scripts/ci/gitleaks-history.sh`, `actionlint .github/workflows/ci.yml`.
Os testes usam apenas executáveis mockados; não puxam imagem nem fazem scan real.
Review independente e CI no SHA publicado continuam necessários. O retry já em
andamento não foi alterado por este patch. Rollback: reverter apenas este passo
e os arquivos adicionados em mudança revisada; não desabilitar o gate.
