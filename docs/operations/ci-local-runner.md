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
O estágio `moby-backport` prepara apenas fontes e `go.mod`, também de forma
nativa. Isso evita o SIGSEGV de QEMU observado na verificação do patch; o
checksum, `git apply --check`, aplicação e verificação do conteúdo permanecem
obrigatórios. Nenhum binário nativo desse estágio é copiado à imagem final.
O rebuild Alloy alinha `golang.org/x/mod` em `v0.41.0`, mínimo exigido
indiretamente por `golang.org/x/net v0.60.0`. Fixar `v0.40.0` junto desse
`x/net` torna o comando `go get` incompatível e interrompe o build.
O source Moby é `v28.5.2` (`89c5e8fd66634b6128fc4c0e6f1236e2540e46e0`)
com backport de `e89edb19ad7de0407a5d31e3111cb01aa10b5a38`, não uma release
`v29.3.1`. Os testes upstream do limite de body e sua preservação são
aplicados com checksum e executados junto da suíte de autorização legada.
Um `replace` local pode ocultar a versão no scanner: seu exit code sozinho
não comprova avaliação do Moby. Exigir inventário/proveniência e análise de
aplicabilidade do backport antes da aprovação de segurança ou do merge.
O rebuild Beyla atualiza `x/crypto`, `x/mod`, `x/net`, `x/text` e gRPC para
as versões corrigidas, regenerando o vendor **antes** dos patches OBI.
Inicializar antes o submódulo OBI no commit exato registrado pelo Beyla,
`d4a4eaae78ceb3053343c15d1d2e651bc362927e`, pois `go.mod` usa `.obi-src`.
Depois exige `git apply --check` e os testes existentes de identidade e
descoberta, evitando que `go mod vendor` apague as correções locais.
O scan completo também exige Cilium eBPF `v0.22.0` e os SDK/exporters OTel
`v1.45.0` para corrigir achados LOW. O aviso OpenPGP sem versão corrigida
exige avaliação do grafo real, conservado em
`/usr/share/sentinelops/beyla-security`; não declarar zero em todas as
severidades apenas por satisfazer o gate HIGH/CRITICAL.
O patch de compatibilidade OBI remove as duas chamadas obsoletas a
`btf.FlushKernelSpec`: Cilium `v0.22.0` não mantém mais esse cache global e
usa caches locais/cleanup. A carga dos tracers permanece intacta; exigir
aplicação do patch, testes e build após a migração, sem baixar a versão segura.
O rebuild Alloy conserva em `/usr/share/sentinelops/alloy-security` o grafo
de pacotes do collector após geração, módulos selecionados, metadados Go,
SHA base Moby, resultado JSON da suíte AuthZ e hashes do binário/fontes.
Esses arquivos são evidência bruta, não uma aprovação automática: comparar
tags/CGO/arquitetura com o binário, ligar os dados ao digest final e avaliar
cada advisory antes do merge. Ausência ou inconsistência mantém o bloqueio.
Os JSON de `go list` são fluxos de objetos, não arrays; consumir até EOF.
A captura usa resolução somente leitura e a arquitetura lida do binário,
exigindo correspondência com a toolchain do build e Linux/CGO=1. A plataforma
dos testes AuthZ nativos é registrada separadamente da imagem AMD64.
O rebuild Alloy atualiza todos os pacotes Ubuntu da base na mesma release,
além dos SDK/exporters trace OTel `v1.45.0` e log `v0.21.0`, para os achados
LOW/MEDIUM com correção disponível. Repetir o scan completo após os upgrades;
achados sem versão corrigida continuam visíveis e exigem avaliação própria.
Manter o conjunto de logs coerente: exporters HTTP/gRPC/stdout em `v0.21.0`
e bridge `otelzap v0.20.0`. Misturar a API log nova com esses consumidores
antigos falha ao compilar tipos `KeyValue`/`Value`; não remover logging como
forma de contornar essa incompatibilidade.
O alerta Snowflake `CVE-2026-86597` exige a migração real dos dois
consumidores para `gosnowflake/v2 v2.2.0`: exporter Grafana no commit
`5be547ff96d8` e SQL Exporter no commit `25d03ba286a6`. Não declarar uma
versão v1 artificialmente corrigida; verificar ausência do driver v1 no
binário e preservar testes de DSN, autenticação e registro do driver.
Exigir os testes dos seis pacotes consumidores/integrações e os testes
oficiais internos de DSN e redaction do SDK v2.2.0. Estes últimos executam
com rede de módulos desligada após preparação, sem credenciais reais;
não equivalem a autenticação ou scrape em uma conta Snowflake de produção.

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
