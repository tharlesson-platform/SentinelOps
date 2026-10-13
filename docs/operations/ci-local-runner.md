# CI local isolado do SentinelOps

O workflow `ci` exige um runner local Linux x64 com as labels
`self-hosted`, `Linux`, `X64` e `sentinelops-ci-isolated`. Não há fallback
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
