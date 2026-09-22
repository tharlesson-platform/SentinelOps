# Beyla: identidade Docker e seleção de contêineres

O patch é restrito ao Beyla 3.15.0, commit upstream
`bc5a6e8a045284fdc74b394371c4d4002b9af519`, em Linux amd64. Reconhece o
cgroup `/docker/<64 hex>` observado no easy-vm e usa a label explícita de
serviço Swarm quando não existe serviço Compose. Não deriva serviço do nome
de uma task. A revisão `.2` também corrige a herança de seleção entre processos:
um filho pode herdar a porta de escuta do pai, mas precisa satisfazer novamente
executável, argumentos, PID explícito, linguagem, metadata, labels/annotations
e limite de contêiner. Exclusões são reaplicadas ao filho; regras de enriquecimento
de logs não podem ampliar essa seleção. Erro ou namespace vazio impedem seleção
quando `containers_only` está ativo.

Aplicar `deploy/agents/linux/beyla-container-discovery.yml`, com descoberta
por `container_name: '?*'`, somente com
imagem `.2` revisada e validada em piloto **nos dois hosts**. O runtime anterior
aceitava filhos via `ProcessHistory[PPid]` mesmo quando metadata/exclusões falhavam.
Os testes de regressão reproduzem essa falha no original e passam com o patch.
O arquivo `beyla.yml` legado permanece compatível com o instalador atual e sua
lista limitada de portas. `--with-beyla` não ativa a descoberta global nem
instala automaticamente proxy metadata ou imagem `.2`.

## Construção e manutenção

Executar na raiz do repositório (o build incorpora a configuração e o contrato
versionados; a configuração não é copiada à imagem final):

```sh
docker buildx build --platform linux/amd64 --load \
  -f deploy/beyla/Dockerfile.patched -t sentinelops-beyla:3.15.0-patched.2 .
docker image inspect sentinelops-beyla:3.15.0-patched.2
```

O build confere o commit upstream, aplica `docker-identity.patch` e
`container-boundary.patch`, e executa os testes de identidade e do matcher real
antes de compilar. O contrato está em
`tests/operations/fixtures/beyla-discovery-contract_test.go.txt`: a extensão evita
tratar o teste de um módulo externo como pacote do SentinelOps; o build o instala
como teste Go no pacote de descoberta. As imagens base têm digest fixo.
Os objetos BPF e JAR vêm do vendor desse commit; não são uma reprodução
binária dos embeds da imagem oficial. Atualizar o upstream exige reavaliar
o patch, os testes e o comportamento Java/eBPF em piloto antes da promoção.

Histórico para rollback: no piloto easy-vm de 2026-09-21, o image ID Linux `.1` foi
`sha256:120b676a4e1ff4c3204d2856d4f2b050dc1528d097ff172ecdf214b7b451fc55`.
O digest do índice OCI de um buildx pode ser diferente do image ID visto
pelo daemon Linux. O tqi-platform usava upstream 3.15.0 com ID
`sha256:8ff0dcb4aa31fab39ba0b40715d0c0441d4522b43fb7886768ec280cc401dd69`.
Os valores históricos não são o digest da nova imagem `.2`. Após o build,
conferir o novo ID efetivamente carregado e fixá-lo no overlay; exemplo histórico
de rollback do easy-vm (não usar este ID para promover `.2`):

```yaml
services:
  beyla:
    image: sha256:120b676a4e1ff4c3204d2856d4f2b050dc1528d097ff172ecdf214b7b451fc55
    pull_policy: never
```

Para promover `.2`, usar `docker-compose.beyla-container-discovery.yml` por
último, após o Compose base e o de docker-read (e qualquer overlay de imagem
anterior). A variável distinta `SENTINEL_BEYLA_DISCOVERY_IMAGE` é obrigatória,
sem fallback, e deve conter o digest da imagem `.2` carregada e conferida;
`pull_policy: never` impede resolução silenciosa por pull. O overlay fornece
o mount do YAML novo e o socket do proxy metadata, sem socket Docker real.
Conferir versão/revisão da imagem antes de configurar a variável.

Na pasta `deploy/agents/linux` de cada host, após definir essa variável:

```sh
docker compose -f docker-compose.yml -f docker-compose.docker-read.yml \
  -f docker-compose.beyla-container-discovery.yml config --quiet
docker compose -f docker-compose.yml -f docker-compose.docker-read.yml \
  -f docker-compose.beyla-container-discovery.yml up -d --no-deps beyla
```

Preservar os demais overlays efetivos quando existirem, colocando o de
descoberta por último. Não copiar o conteúdo novo sobre `beyla.yml`.
Recriar exclusivamente `beyla` com `up -d --no-deps beyla`. Preservar todas
as opções de rede, PID, mounts, capabilities e os containers de negócio.

## Aceitação e rollback

- Guardar imagem e configuração anteriores; confirmar apenas um Beyla ativo.
- Antes da troca, testar metadata pelo proxy Unix com nome/ID iguais ao Docker,
  incluindo aplicação em 8090 e RabbitMQ 15672/15692. Após trocar apenas Beyla,
  provar descoberta desses recursos e ausência dos coletores/host no escopo.
  Validar namespace sob `docker-default`; não abrir AppArmor ou privilégios para
  contornar negação. Se metadata/namespace não puderem ser lidos, não promover.
- Conferir host, serviço e prefixo único do container nas métricas e traces.
- Fazer GET de leitura em rota conhecida dos serviços Java e validar a
  captura da resposta real. HTTP 401/404 comprova protocolo, não saúde da aplicação.
- Comparar erros de injeção, CPU, memória e StartedAt dos containers de negócio.
- Para rollback, remover o overlay de descoberta global e restaurar o conjunto
  anterior de overlays/YAML, recriando apenas Beyla com a imagem anterior
  retida de cada host, repetindo as verificações. Não ampliar
  permissões para contornar limitações de instrumentação Java ou Node.

O piloto histórico `.1` confirmou identidade de sete serviços Java nas métricas e no Tempo.
Persistem nove erros de instrumentação já observados antes do patch; não há
garantia de spans internos SDK nem de cobertura de todos os protocolos.
A revisão `.2` e a seleção global são uma entrega separada da API/catalogação;
testes locais não constituem evidência de build publicado ou aceite ao vivo.
