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

Evidências de testes usam fixtures. Publicação e aceite autenticado serão registrados separadamente após a execução.
