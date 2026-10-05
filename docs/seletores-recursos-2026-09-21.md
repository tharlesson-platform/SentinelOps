# Seleção direta de recursos — 21/09/2026

Métricas, APM, Logs e Requisições passam a oferecer um seletor de recurso na própria tela, com busca por nome. A escolha explícita substitui o contexto anterior e preserva o período. URL, recarregamento e Voltar mantêm a seleção.

- **Métricas:** lista servidores e containers do inventário Prometheus. Containers homônimos são diferenciados pelo servidor e consultados com a identidade do exporter correspondente.
- **APM:** lista aplicações com ambiente e servidor. A identidade completa, inclusive dimensões ausentes, fica registrada em `apmResource` na URL; escolher uma aplicação sem ambiente não inclui outras instâncias dela. O cálculo continua usando os últimos cinco minutos, conforme indicado na tela.
- **Logs:** lista servidores, aplicações e containers presentes nos resultados reais do Loki. ID de container e nome são campos distintos.
- **Requisições:** lista serviços encontrados no Tempo. Um ID de container vindo dos logs sem nome confirmado não é enviado como `container.name` e não amplia silenciosamente a consulta.

Em Logs e Requisições, **Listar recursos de todas as fontes** executa uma descoberta explícita no período, limitada a 300 eventos ou 100 requisições. Ela atualiza as opções sem alterar o escopo nem os resultados da investigação. A lista é uma amostra, não um inventário completo. O formulário **Selecionar por nome ou combinar filtros** permite informar identidades da fonte que não apareçam nessa amostra. Aplicar substitui os filtros anteriores; campos vazios não restringem a consulta. Não se infere `host_name` a partir de `instance`.

Estados de carregamento, falha, ausência e lista parcial permanecem visíveis. Seleções fora da lista continuam identificadas; não são limpas automaticamente. O filtro de ambiente APM não é aplicado ao Tempo, conforme aviso existente.

## Validação local

- 25 testes unitários web aprovados.
- 31 testes Playwright aprovados, incluindo seis novos cenários de seletores: entrada direta, homônimos, URL/reload/Voltar, descoberta explícita, filtros exatos, resposta antiga, APM com dimensões ausentes, ID sem nome e layout mobile de 390px.
- Build com `VITE_BASE_PATH=/sentinelops/` aprovado.
- Alteração limitada à interface; nenhuma mudança de API, collector ou serviço de negócio.

As evidências dos testes locais usam fixtures. A revisão SRE independente aprovou a entrega local e as correções de identidade.

## Publicação verificada

A interface `de08da8211c2098b20048a3db9b372997e96bc31` foi publicada às **14:58:59 UTC**. Commit e push foram conferidos no remoto. O build foi reproduzido no servidor após verificar a árvore completa da fonte; o transporte ocorreu pela sessão SSH previamente autenticada.

- Cinco arquivos conferidos por SHA-256 via HTTPS no servidor e novamente no Mac, com MIME correto.
- Configuração runtime preservada; 23 containers mantiveram identidade, imagem, reinícios e estado. As duas réplicas da API permaneceram saudáveis.
- Assets instalados antes da troca atômica do índice. Backup `index-before.html` e registro `published.json` em `/opt/sentinelops/releases/web-selectors-de08da8/`; assets anteriores preservados para rollback.
- Marcador web atualizado; versão da API continua `8b4abdb`.
- Sem execução de CI remoto observada para este SHA; os resultados citados acima são locais.

| Artefato | SHA-256 |
|---|---|
| Árvore fonte web | `e4931df809740b5e47e7c10a9512c67d83d7ba0cca5ec023488a0d2ac266c8c0` |
| HTML | `5563ab468e666201b6359adc1f3f692c39ba639c4bd22f6d88f3d028fff7cef7` |
| JavaScript `index-BkLCFd6Z.js` | `b519ec2694b17b09c863243951db95995e68ec72318e4cc7280e97e71ede4126` |
| CSS `index-DSff4EJy.css` | `2199943481ceea8676a38aa8475b001196778c6f7dc6940c7c17c95c4edd7b13` |

## Aceite em produção

O Safari carregou o novo seletor de Métricas após a publicação, mas a API informou sessão expirada. O pedido real de Touch ID para o login salvo está aberto; a jornada autenticada dos quatro seletores ainda não deve ser declarada aprovada.


Às 15:02:47 UTC, a API autenticada confirmou 2 servidores, 46 containers e 5 aplicações APM. Métricas de tqi-platform e easy-vm retornaram 11 grupos cada, com fonte disponível. A descoberta Loki incluiu os dois hosts; a consulta limitada a easy-vm retornou 300 eventos, todos com o host solicitado. Tempo retornou serviços reais e a seleção de um deles retornou 100 requisições com fonte disponível. Os limites de 300/100 foram atingidos; esses números não representam o total de eventos ou requisições. Credenciais e token foram usados apenas em memória no servidor, sem exposição nos registros.
