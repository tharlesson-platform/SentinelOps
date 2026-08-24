# Bootstrap Linux do zero ao ambiente funcional

Este fluxo instala todas as dependências, cria secrets e PKI, constrói imagens
fixadas, aplica migrações, sobe a plataforma com duas APIs e dois workers,
carrega a demonstração e comprova coleta, ingestão, persistência e dashboards.

## Distribuições e capacidade

O detector suporta apt, dnf, yum, microdnf, zypper, apk e pacman. A matriz automatizada
cobre Ubuntu, Debian, Rocky Linux, Fedora, openSUSE e Alpine em x86_64 ou arm64.
O perfil local exige 8 GiB de RAM e 40 GiB livres. Produção HA deve usar Helm e
backends externos conforme o runbook de produção.

## Instalação a partir de um release já transferido

Extraia o release e execute:

    sudo ./bootstrap-linux.sh

O padrão publica Web, Grafana e backends apenas em loopback. Para receber
collectors pela rede privada:

    sudo ./bootstrap-linux.sh \
      --web-bind 10.10.20.15 \
      --ingest-bind 10.10.20.15 \
      --ingest-server-name ingest.internal.example

Abra no firewall somente TCP 8443 dos segmentos monitorados. A porta 8443 exige
mTLS. Não publique 3000, 3001, 8080, 9090, 3100, 3200 ou 4318 diretamente.

## Instalação a partir de release remoto

O download exige HTTPS e SHA-256 explícito; archive com caminho absoluto ou
travessia de diretório é recusado:

    sudo ./bootstrap-linux.sh \
      --source-url https://releases.example/sentinelops-0.2.0.tar.gz \
      --sha256 HASH_DE_64_CARACTERES \
      --install-dir /opt/sentinelops

O resultado funcional fica em:

- SentinelOps Web: porta 3000;
- Grafana: porta 3001;
- API edge: porta 8080;
- aplicação mock: porta 8090;
- gateway de ingestão mTLS: porta 8443;
- Temporal UI: porta 8088.

A evidência sanitizada é gravada em artifacts/installation. Credenciais ficam
somente no arquivo .env com modo 0600 e podem ser consultadas localmente com
make credentials.

## Fases e retomada

    ./scripts/install-linux-server.sh --phase preflight
    ./scripts/install-linux-server.sh --phase runtime --install-runtime
    ./scripts/install-linux-server.sh --phase configure
    ./scripts/install-linux-server.sh --phase deploy
    ./scripts/install-linux-server.sh --phase seed
    ./scripts/install-linux-server.sh --phase verify
    ./scripts/install-linux-server.sh --phase service

Systemd e OpenRC são configurados automaticamente. Em outro init, as restart
policies do Compose mantêm os containers e o instalador informa a integração
manual necessária.

## Critérios de aceite

    make prove-local
    make prove-dashboards
    make prove-apm
    make prove-ha
    make test-synthetics

Os dashboards Linux Hosts, Application Overview, APM e Docker devem aparecer
provisionados e apresentar o host e as aplicações controladas do laboratório.
