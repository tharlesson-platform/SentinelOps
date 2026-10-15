# CI local isolado do SentinelOps

Os workflows versionados neste repositório exigem uma VM local Linux ARM64 registrada com
`--no-default-labels` e somente `sentinelops-run-<run_id>`, específica da
execução aprovada. Não adicionar labels genéricas ou compartilhadas.
Não há fallback
para runners hospedados. A ausência desse runner deixa o job aguardando;
nunca constitui validação bem-sucedida.

O **GitHub Code Quality gerenciado** é uma execução separada, com path
`dynamic/github-code-quality/codeql`; seus runners não são determinados
pelos YAML acima. Na auditoria de 2026-10-13, ele ainda usava `ubuntu-latest`
hospedado no SentinelOps. Portanto, checks `Analyze` verdes não comprovam
que essa análise ocorreu localmente. A migração desse serviço permanece
pendente; não declarar toda a organização local antes do readback dos jobs.

Consultar sua configuração pelo endpoint `code-quality/setup` com versão
API `2026-03-10`, não pelo `code-scanning/default-setup`. A
[API oficial de Code Quality](https://docs.github.com/en/rest/code-quality/code-quality?apiVersion=2026-03-10)
suporta `runner_type: labeled` e `runner_label`. Preservar estado, linguagens,
agenda e opções existentes; não desabilitar análises para tornar o gate verde.
Um label fixo do serviço não equivale ao label exclusivo por run do CI:
submeter o agendamento/isolamento à revisão independente antes de mudar.
Os quatro gates privados de Code Security mantidos pendentes por decisão
do usuário não autorizam ativação de licença ou cobrança.

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

O builder de `Dockerfile.app` usa `FROM --platform=$BUILDPLATFORM`, preservando
a toolchain Go `1.27.2` e seu digest. Com `CGO_ENABLED=0`, os argumentos
automáticos `TARGETOS` e `TARGETARCH` definem explicitamente o binário de saída;
a imagem Alpine final continua na plataforma solicitada. Esse é o padrão de
[cross-compilation documentado pelo Docker](https://docs.docker.com/build/building/multi-platform/#cross-compilation).
No job `115528669280`, o compilador AMD64 foi observado sob QEMU em uma VM
ARM64, com longa inatividade. Isso confirma emulação, mas não determina a
causa exata da inatividade nem demonstra deadlock. Validar o builder nativo,
os sete apps AMD64, ao menos a API ARM64, metadados Go/ELF, tempos e scans
completos antes de publicar. Os builds de release AMD64/ARM64 e seus gates
permanecem obrigatórios; cross-compilation não comprova funcionamento em produção.

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
`5be547ff96d8` e SQL Exporter no commit `7186d9947bc1`. Não declarar uma
versão v1 artificialmente corrigida; verificar ausência do driver v1 no
binário e preservar testes de DSN, autenticação e registro do driver.
Exigir a compilação/testes dos sete pacotes consumidores/config/integrações e os testes
oficiais internos de DSN e redaction do SDK v2.2.0. Estes últimos executam
com rede de módulos desligada após preparação, sem credenciais reais;
não equivalem a autenticação ou scrape em uma conta Snowflake de produção.
O commit SQL selecionado é a migração upstream real para `/v2`, com o SDK
explicitamente elevado a `v2.2.0` corrigido. Evita introduzir o upgrade
Kubernetes da release posterior que quebra os informers do collector;
não força um `client-go` inferior às dependências nem simula versão segura.

## Gate complementar do SDK Moby

O job Alloy executa `scripts/ci/alloy-security.sh` após o scanner obrigatório.
Extrai o binário e inventário de um container **nunca iniciado**, sem rede ou
mounts do host; confere hash, versão real, plataforma, patches e testes AuthZ.
Exige grafo completo até EOF, sem erros, imports ausentes ou entrada diferente
do collector efetivamente compilado (`github.com/grafana/alloy/otel_engine`).
Confere também `Deps` e alcance transitivo, além de comparar o registro com
`go version -m` lido do binário real por uma toolchain Docker nativa fixada,
offline e somente leitura. O alvo AMD64 nunca é executado nessa leitura.

Gera um CycloneDX complementar do SDK `v28.5.2+incompatible` para que o Trivy
também examine o `replace` local. Não modifica nem substitui o SBOM completo.
As quatro CVEs daemon/plugin avaliadas podem ser consideradas não aplicáveis
**somente se os pacotes vulneráveis estiverem ausentes do grafo deste artefato**.
Advisory novo, versão divergente, evidência ausente ou pacote vulnerável
presente mantém o gate vermelho. O scan completo adicional rejeita HIGH e
CRITICAL inclusive sem patch disponível. Não há VEX automático nem arquivo
de ignore; os relatórios brutos continuam intactos, inclusive os residuais
LOW/MEDIUM/UNKNOWN. Isso não avalia o daemon Docker externo nem prova runtime.

`image-alloy.json` conserva avaliação e todas as evidências JSON/texto em
`gzip+base64`, com SHA256 individual do conteúdo original. É enviado ao
serviço local de artifacts com capability temporária e nome restrito, também
em falhas de avaliação. Um build/coleta indisponível produz diagnóstico
vermelho, não avaliação bem-sucedida. O uploader compara SHA256, tamanho,
nome, repositório, run e source SHA do receipt antes de aceitar persistência.
O empacotador admite somente membros previstos e rejeita symlinks.
A identidade registrada é o **image ID local**, não evidência de publicação
no registry. O engine clássico identifica o config por SHA-256; o containerd
pode identificar o manifesto. `Trivy.Metadata.ImageID` pode expor config ou
manifesto conforme o backend/caminho de coleta; não inferir essa superfície
pela versão do scanner sem prova. Comparar os IDs sem distinguir a cadeia
pode produzir um falso mismatch.

O shell captura `docker image save "$image_id"` em stream, sem reter outro
tar completo, extrair camadas ou executar a imagem. Preserva somente os bytes
exatos de config/manifesto em `image-config.json` e `image-manifest.json`.
No engine clássico, o hash do config deve ser o `.Id`, e o manifest do save
deve referenciar esse config. No containerd, o hash do manifesto deve ser o
`.Id` e o Descriptor Docker; o descriptor de config deve conferir hash e
tamanho do config capturado. Index multiarch não é aceito silenciosamente:
este gate exige manifesto linux/amd64 individual e falha se receber outro formato.

Os DiffIDs **ordenados** do config devem conferir com Docker RootFS,
Trivy DiffIDs, ImageConfig.rootfs e cada Layers.DiffID. No containerd, também
confere os digests das camadas Trivy com os descriptors do manifesto.
Somente **depois** de validar essa cadeia completa, o ImageID do Trivy deve
ser igual ao configDigest ou ao manifestDigest calculados dos bytes capturados.
Nenhum ID externo, tag, annotation ou digest de manifesto não verificado é
fallback. A avaliação registra `trivyImageID` e `trivyImageIDSurface`.
DiffID é hash da camada descomprimida, não sinônimo do digest da camada
comprimida. Plataforma e configuração de execução também precisam conferir.
Ausência, troca, truncamento ou ambiguidade de identidade mantém o gate vermelho;
não substitui a comparação por tags nem por igualdade parcial das camadas.
O stream permite até 10.000 membros, metadata individual até 1 MiB e
metadata candidata agregada até 16 MiB; camadas grandes passam sem serem
armazenadas em memória. O custo adicional é ler uma exportação local da imagem.
Config/manifesto brutos continuam no bundle diagnóstico, inclusive em falhas.
Não reutilizar o diretório de outro build.

Referências primárias: [OCI config/DiffIDs](https://github.com/opencontainers/image-spec/blob/main/config.md),
[OCI manifesto/config descriptor](https://github.com/opencontainers/image-spec/blob/main/manifest.md).
Reexecutar os testes positivos/negativos de `test_alloy_security.py` ao
alterar o gate, e submeter mudanças de aplicabilidade à revisão independente.

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
