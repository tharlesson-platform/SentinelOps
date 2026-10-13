# CI local isolado do SentinelOps

Os workflows exigem uma VM local Linux ARM64 registrada com
`--no-default-labels` e somente `sentinelops-run-<run_id>`, específica da
execução aprovada. Não adicionar labels genéricas ou compartilhadas.
Não há fallback
para runners hospedados. A ausência desse runner deixa o job aguardando;
nunca constitui validação bem-sucedida.

Como este repositório é público, não habilitar acesso ao pool local
compartilhado. Provisionar uma VM descartável dedicada, sem credenciais
de produção, volumes do host ou acesso às redes internas. O grupo deve
autorizar somente este repositório, exigir aprovação para contribuições
externas e destruir a VM após cada job. Docker e as ferramentas de CI
devem existir dentro dessa VM; não expor o socket Docker do computador.

A imagem `postgres` usada pelos testes é temporária. Não usar o banco de
produção. O workflow mantém testes, `govulncheck`, scanners de secrets,
validações de Helm/Terraform e scans das imagens como gates obrigatórios.

O estágio Node do frontend usa `$BUILDPLATFORM`: gera assets estáticos na
arquitetura nativa do builder. Isso evita o SIGSEGV reproduzido ao executar
JavaScript Node AMD64 sob QEMU no runner ARM64. A imagem final Nginx continua
no alvo solicitado (`linux/amd64` no CI), com usuário sem privilégios,
atualizações de pacotes e scans preservados. Não remover o suporte AMD64.
O PID do Nginx usa `/tmp/nginx.pid`, pois o usuário `nginx` não pode escrever
em `/run`. Validar resposta HTTP e assets da imagem sem promover a produção.
O job da imagem web executa `scripts/ci/web-image-smoke.sh`: exige alvo AMD64,
usuário `nginx`, PID gravável, HTML e assets JS/CSS com tipos corretos. Usa
porta efêmera apenas em loopback e remove seu container ao terminar. Esse
smoke não comprova a integração com a API nem o funcionamento em produção.
O estágio `source-ui` do Alloy também usa `$BUILDPLATFORM` para seus assets
estáticos; os estágios de compilação Go/CGO e a imagem final permanecem no
alvo solicitado. A validação isolada da UI não substitui build e scan completos.

## Correção de segurança da toolchain

Os jobs anteriores usavam Go 1.26.6 e falharam com 11 vulnerabilidades
alcançáveis da biblioteca padrão. Go 1.26.9 é a versão corrigida da mesma
série; `go.mod`, CI, harness e builds auxiliares passam a usá-la. O build
da aplicação permanece na série 1.27, atualizado para 1.27.2. As imagens
continuam fixadas em digests verificados no registry oficial.

Após corrigir o compilador, o scan local revelou mais quatro achados
alcançáveis em `golang.org/x/net` 0.58.0. A dependência da aplicação e dos
builds Caddy/Alloy foi atualizada para a versão corrigida 0.60.0.

Antes do merge, executar os gates no SHA atual e revisar os diffs. Builds
locais não comprovam aprovação de CI nem prontidão para produção. Não
reverter para as toolchains vulneráveis como rollback; um rollback da
aplicação deve preservar o compilador corrigido.
