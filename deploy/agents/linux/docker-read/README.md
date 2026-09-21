# Identidade Docker e leitura do driver local

Este perfil complementar mantém o Docker socket real fora do Alloy/Beyla.
Dois proxies Unix independentes expõem apenas metadata sanitizada ou metadata
mais leitura de logs de containers cujo driver é `local`. Não há porta TCP,
mutação Docker, `Env`, comandos, health output, leitura de arquivos ou imagem.
O processo de proxy roda no host autorizado e acessa o socket local do Docker;
a fronteira de autorização é a whitelist Python, não o mount `:ro` do socket.

## Instalação e gates

1. Conferir hostname e daemon; registrar IDs, StartedAt, reinícios, saúde,
   drivers, disco e filas. Guardar configuração anterior sem divulgar `.env`.
2. Instalar `proxy.py` e `inventory.py` em `/opt/sentinelops-docker-read` como
   root, sem permissão de escrita para clientes. Confirmar o grupo GID473 no
   host; quando não existir, criar `groupadd --system --gid 473 sentinelops-read`
   sem adicionar usuários. O systemd precisa resolver esse grupo. Criar
   `/etc/sentinelops-docker-read.env` com `SENTINEL_HOST_NAME=<host autorizado>`.
3. Instalar as units deste diretório. Criar
   `/var/lib/sentinelops-docker-read/inventory` como root:473, 0750; publicar
   inventário com o script antes de habilitar o timer. JSON root:473, 0640.
   O diretório é montado somente para leitura nos consumidores.
4. Validar cada candidato com o binário Alloy ativo e mesmo ambiente. Aplicar
   a identidade OTLP por reload preservando inode. O Alloy central deve
   sanitizar traces antes do tail sampling e reter respostas 400–599 com os
   nomes de atributo atual e legado, além do status ERROR.
5. Iniciar somente `sentinelops-docker-read@metadata.service` e o timer.
   Conferir `HEAD /_ping`, inspect permitido e negação de `/logs` e POST.
6. Criar backup do `beyla.yml` ativo; aplicar portas HTTP observadas e o Compose
   complementar. Validar `docker compose -f docker-compose.yml
   -f docker-compose.docker-read.yml config --quiet`. Recriar **somente Beyla**
   com `up -d --no-deps beyla`. Não executar `down`, `down -v` nem up irrestrito.
   Conferir nomes reais de serviço, host, ambiente e container nos sinais.
7. Para logs JSON, o mount novo exige recriar **somente container-logs**, com
   mesmo volume, storage path e IDs dos componentes. Antes de parar, conferir
   ausência de retries/rejeições, guardar posições para investigação e deixar
   shutdown gracioso drenar. Não restaurar posições antigas no rollback.
8. Para driver `local`, executar antes o contrato nativo (framing, silêncio,
   restart) e o ensaio da fila persistente com destino indisponível/recuperação. Somente na
   primeira instalação criar `enrolled.json` vazio e `activated-at.json` com
   o epoch de ativação (root:root, 0600, fsync). Ausência
   ou corrupção em instalação existente exige recuperação do estado, nunca
   novo bootstrap automático. Iniciar `sentinelops-docker-read@logs.service`.
   Criar `local-alloy` com UID/GID473 e modo0750; habilitar exclusivamente
   `--profile local-logs up -d --no-deps local-container-logs`.

Os sockets ficam em diretórios separados `sentinelops-docker-metadata` e
`sentinelops-docker-logs` sob `/run`, root:473, 0750; cada cliente monta apenas
seu diretório. Reinício do proxy pode trocar o inode do socket sem invalidar
seu mount. Não trocar mounts por arquivos de socket individuais.

## Semântica e limites

- Enrollment é durável. Cada ID novo inicia em `max(activated-at, Created)`
  obtido do Docker, com resolução de segundos: containers já existentes não importam histórico anterior à
  ativação; novos/recriados preservam logs de inicialização mesmo se descobertos
  depois. Não há backfill antigo. A retenção do Docker limita o que ainda pode
  ser recuperado. Na migração do piloto, a ativação deriva do menor enrollment
  já persistido, preservando todos os offsets existentes.
- O reader nativo usa cursor em segundos; reconexões podem repetir eventos.
  Posição lida não é confirmação de entrega. A fila OTLP usa armazenamento persistente com fsync, capacidade lógica de
  64 MiB e bloqueio por saturação. O limite não é uma quota física do banco.
  A recuperação começa após enqueue; a fonte pode avançar antes desse fsync.
  Erros permanentes e falhas de disco ainda podem descartar registros. O batch
  central confirma recebimento antes da persistência final no Loki. Não há
  promessa de at-least-once ponta a ponta. Métricas de enqueue, falhas, fila
  e disco devem ser acompanhadas. O WAL experimental de loki.write foi
  reprovado no ensaio de crash e não é usado neste perfil.
- Não aplicar `tail=N` como paginação, não apagar WAL/posições e não mudar
  labels de um mesmo target para reiniciar sua leitura. Containers parados
  permanecem no inventário enquanto existirem no Docker para drenar seu final.
- `stdout` e `stderr` são separados para containers sem TTY. TTY os mistura.
- Metadata usa nome explícito OTEL, Compose service, Swarm service e finalmente
  nome do container. Confirmar equivalência com o valor emitido pelo Beyla/SDK.
  O Beyla3.15 emite ID abreviado: cruzar por prefixo único dentro do host ou por
  host e nome; não comparar ID12 e ID64 por igualdade.
- eBPF fornece spans HTTP/gRPC de protocolo; não equivale a spans internos SDK.
  Sem tráfego ou protocolo compatível não há prova de trace. Banco/cache e
  workers sem HTTP podem ter métricas/logs sem requisições APM.
- A sanitização remove atributos definidos de credenciais, cookies, usuário e
  URLs completas/query. Não é um detector universal de dados pessoais. A
  política atual de logs descarta linhas com padrões de segredo; esses descartes
  precisam permanecer explícitos na evidência de cobertura de erros.

## Rollback

Parar apenas o novo `local-container-logs` e proxy de logs se esse perfil falhar;
preservar `enrolled.json` e `local-alloy`. Restaurar os arquivos anteriores do
coletor JSON e recriar só `container-logs` pelo Compose original. Restaurar
`beyla.yml` e recriar só Beyla pelo Compose original. Restaurar o conteúdo dos
Alloys local/central e fazer reload com o mesmo inode. Nunca restaurar uma cópia
antiga de WAL/posições sobre o processo ativo. Preservar scripts/estado para
investigação e confirmar que IDs/StartedAt de negócio não mudaram.

O alerta `LocalLogCollectorMissing` declara explicitamente `tqi-platform` como
host habilitado e usa o Prometheus central para verificar ausência de heartbeat ou scrape continuamente indisponível por três minutos.
Ao habilitar o perfil em outro host, ampliar essa lista no mesmo rollout. Os
alertas de fila não substituem esse controle externo nem o alerta de disco Linux.
