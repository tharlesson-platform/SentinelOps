# Beyla: identidade Docker no piloto easy-vm

O patch é restrito ao Beyla 3.15.0, commit upstream
`bc5a6e8a045284fdc74b394371c4d4002b9af519`, em Linux amd64. Reconhece o
cgroup `/docker/<64 hex>` observado no easy-vm e usa a label explícita de
serviço Swarm quando não existe serviço Compose. Não deriva serviço do nome
de uma task. O tqi-platform permanece com a imagem anterior.

## Construção e manutenção

Executar na raiz deste diretório:

```sh
docker buildx build --platform linux/amd64 --load \
  -f Dockerfile.patched -t sentinelops-beyla:3.15.0-patched.1 .
docker image inspect sentinelops-beyla:3.15.0-patched.1
```

O build confere o commit upstream, aplica o patch e executa os testes dos
dois pacotes alterados antes de compilar. As imagens base têm digest fixo.
Os objetos BPF e JAR vêm do vendor desse commit; não são uma reprodução
binária dos embeds da imagem oficial. Atualizar o upstream exige reavaliar
o patch, os testes e o comportamento Java/eBPF em piloto antes da promoção.

No piloto de 2026-09-21, o image ID Linux carregado foi
`sha256:120b676a4e1ff4c3204d2856d4f2b050dc1528d097ff172ecdf214b7b451fc55`.
O digest do índice OCI de um buildx pode ser diferente do image ID visto
pelo daemon Linux. Conferir o ID efetivamente carregado e fixá-lo no overlay:

```yaml
services:
  beyla:
    image: sha256:120b676a4e1ff4c3204d2856d4f2b050dc1528d097ff172ecdf214b7b451fc55
    pull_policy: never
```

Aplicar esse overlay por último, após o Compose base e o de docker-read.
Recriar exclusivamente `beyla` com `up -d --no-deps beyla`. Preservar todas
as opções de rede, PID, mounts, capabilities e os containers de negócio.

## Aceitação e rollback

- Guardar imagem e configuração anteriores; confirmar apenas um Beyla ativo.
- Conferir host, serviço e prefixo único do container nas métricas e traces.
- Fazer GET de leitura em rota conhecida dos serviços Java e validar a
  captura da resposta real. HTTP 401/404 comprova protocolo, não saúde da aplicação.
- Comparar erros de injeção, CPU, memória e StartedAt dos containers de negócio.
- Para rollback, remover somente o último overlay, recriar apenas Beyla
  com a imagem anterior retida e repetir as verificações. Não ampliar
  permissões para contornar limitações de instrumentação Java ou Node.

O piloto confirmou identidade de sete serviços Java nas métricas e no Tempo.
Persistem nove erros de instrumentação já observados antes do patch; não há
garantia de spans internos SDK nem de cobertura de todos os protocolos.
