# Implantação da interface TQI e correção APM — 21/09/2026

## Resultado e versão

Deploy manual autorizado concluído às 08:33 BRT (11:33 UTC), com duas réplicas da API saudáveis. Commit de aplicação `3b8192e478444275a9db76aa1a1d4194988730ad`, após a renovação `70ad1bba0b5e9faedef4a46792a3c82911d92ef5`, ambos enviados à branch `fix/linux-vm-bootstrap`. Este relatório não representa uma aprovação geral de produção da plataforma.

O portal e o bundle web foram publicados após a troca gradual da API. Cada réplica nova passou por readiness e autenticação antes da retirada da anterior. Os 21 containers fora da API permaneceram com os mesmos IDs. Configuração de runtime, redes, certificados, autenticação e volumes foram preservados. Nenhum collector de outro host foi atualizado.

| Artefato | Identificação |
|---|---|
| Release no servidor | `/opt/sentinelops/releases/deploy-3b8192e` |
| Imagem nova | `sha256:5408887df9ee8f89f8f61c668b4cb6b2b53ce187b172d8ed06de53743b22d224` |
| Binário Linux amd64 | SHA-256 `1286dc533c2d4da811745aa8609d5aa2bea3f52d70cccb08585e6e9eb40d82b4` |
| JavaScript | `index-DlL5fKyI.js` |
| CSS | `index-Br3Lol8O.css` |
| Imagem anterior preservada | `sentinelops-api:rollback-3b8192e` |
| Backup anterior | `backup/` dentro da release, restrito ao root |

## Evidência de execução

- Compilação e testes Go executados no servidor com imagem local `golang:1.27.1-alpine3.24`: `internal/httpapi` e `internal/telemetryquery` aprovados. O binário substitui somente a aplicação na imagem de runtime existente, preservando usuário, entrypoint, comando e ambiente.
- Validação autenticada após deploy: 12/12 verificações aprovadas, incluindo login, negação sem sessão, inventários, séries reais, exigência de servidor no detalhe de container, ordenação/limite global dos logs e comparação com a baseline. Repetição às 08:35 BRT também passou; duas réplicas saudáveis, sem reinícios.
- Ingresso: 24/24 verificações aprovadas antes e depois, cobrindo HTML/assets, login Grafana, datasources, leituras autenticadas de métricas/logs/traces/perfis, bloqueio sem sessão, issuer Keycloak e catálogo SentinelOps. Não foram executadas escritas de telemetria.
- Inventário observado: 1 servidor, 47 containers, 4 serviços APM e 50 traces na janela consultada. Métricas do servidor: 22 séries e 1.342 pontos; container amostrado: 6 séries e 366 pontos, com timestamps atuais. Isso descreve a consulta, não cobertura completa do ambiente.
- Logs: 20 eventos, duas consultas disjuntas, ordem decrescente e limite global confirmados. IDs completos de log disponíveis em 47 containers; `hostName` ainda ausente nos inventários.
- HTTPS pela VPN: hashes dos seis caminhos publicados (portal, índices web, logo, JS e CSS) conferem com a release. No servidor, o hash de `config.js` permaneceu igual ao backup.
- Navegador real: portal renovado, seus oito destinos e navegação para a nova tela de login confirmados visualmente. A sessão autenticada foi validada por HTTP; a jornada interna autenticada do navegador não foi executada nesta etapa. Os 19 testes de navegador anteriores usam fixtures locais.

## APM: correção e limite da evidência

Antes da preparação, três requisições APM reproduziram HTTP 200 com corpo vazio, acompanhadas de percentis não finitos no Prometheus. A correção omite apenas amostras NaN/infinitas, preservando valores finitos e timestamps; percentis ausentes permanecem nulos. Testes de regressão cobrem esse comportamento.

Na baseline imediatamente anterior ao rollout, os valores não finitos já tinham desaparecido e o APM retornava quatro serviços. Após o deploy, três consultas adicionais retornaram JSON válido e não vazio. Portanto, a evidência live confirma a resposta atual, mas não constitui um experimento antes/depois sob a mesma entrada NaN. A exceção estrita preparada para a falha antiga não precisou ser usada no rollout.

## Evento durante a troca

O monitor interno registrou uma falha em 49 amostras de healthz, às 08:33:19 BRT. Os logs do ingresso e do API edge confirmaram um HTTP 502; no mesmo intervalo, o edge registrou `no live upstreams` e timeout. O monitor gravava exceções como status zero, por isso foi necessário cruzar os logs. As consultas seguintes e as validações finais passaram. Não é uma implantação com indisponibilidade zero comprovada; a duração exata do impacto não foi medida.

O monitor externo pela VPN terminou com 689 consultas, nenhuma falha e latência máxima de 1602 ms, entre `2026-09-21T11:12:32Z` e `2026-09-21T11:37:18Z`. A amostragem externa não capturou o 502 interno e não o invalida.

Antes de outra atualização, aprimorar a drenagem e a espera de descoberta das réplicas no edge, com teste que cubra requisições durante a remoção. Nenhuma configuração de Nginx foi alterada nesta implantação.

## Recuperação e evidências preservadas

A release mantém o backup do portal, bundle/config.js, lock de imagens, configuração ativa Nginx, fontes substituídos e marcador de versão anterior. A imagem antiga foi retida. O procedimento operacional tem rollback para restaurar a imagem, confirmar duas réplicas antigas saudáveis, restaurar HTML/fontes/marcador e preservar volumes. Não houve rollback executado nem ensaio de recuperação completo nesta janela; não apresentar o código de rollback como teste de recuperação realizado.

Os relatórios sanitizados e scripts desta operação ficam na release do servidor e em `artifacts/releases/evidence-3b8192e/` no checkout operacional (ignorados pelo Git). O monitor VPN fica em `artifacts/releases/vpn-http-monitor-2026-09-21.json`. Credenciais, cookies e tokens não foram incluídos nos relatórios nem no repositório público.

## Pendências delimitadas

- Distribuir e validar os perfis Alloy com `host_name` em uma mudança própria de collectors; sem esse label, a UI mantém bloqueada a correlação específica que depende dele.
- Homologar OIDC corporativo e a jornada autenticada completa no navegador em seu escopo próprio. O runtime desta implantação usa autenticação local.
- A investigação de traces ainda não restringe por ambiente APM, conforme aviso da interface.
- Validar a referência visual TQI Bid quando disponibilizada; a identidade aplicada usa o ativo TQI de Orgflow.
- Esta execução manual não comprova pipeline remoto, maturidade geral de produção ou recuperação de desastre.

A evidência local anterior está em [Renovação da interface](renovacao-interface-tqi-2026-09-18.md). A revisão independente de implementação permanece histórica e deve ser lida junto ao resultado operacional desta implantação.

## Revisão independente final

Resultado: **APPROVED WITH CHANGES**, restrito à aceitação deste rollout. O revisor conferiu os relatórios locais e o monitor VPN bruto, sem alteração de arquivos nem acesso remoto. Não identificou bloqueador para manter a versão implantada ou versionar a documentação. A melhoria necessária antes de outro deploy é a convergência de descoberta e drenagem no edge, com teste sob tráfego durante a retirada de réplicas. As pendências de collectors, OIDC e jornada autenticada permanecem delimitadas acima.

## Verificação autenticada posterior no navegador

Após autenticação pelo usuário no Safari, a versão `3b8192e` foi exercitada no navegador publicado. Seleção explícita de servidor/container, troca entre containers homônimos de dois hosts, gráficos e tabela com timestamps, atualização de período, Voltar e Início foram observados. A troca manteve a identidade composta; a atualização avançou o timestamp sem perder o container escolhido.

Quando `host_name` estava ausente, a tela bloqueou a consulta correlacionada. No intervalo observado de 11:44:58–11:45:15 UTC, o ingresso registrou zero chamadas ao endpoint de logs, confirmando ausência de fallback amplo. Essa verificação complementa a limitação de navegador registrada na etapa inicial; OIDC e alterações de collectors continuam fora deste aceite.

A ampliação de cobertura após esta versão está documentada separadamente em [Cobertura nativa de observabilidade](cobertura-observabilidade-2026-09-21.md).
