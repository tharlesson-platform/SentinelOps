# Leitura operacional dos gráficos — 21/09/2026

## Mudança

A interface passa a destacar valores e unidades, com eixos numéricos, legenda com último/mínimo/média/máximo, inspeção por horário usando mouse ou teclado, ocultação de séries e tabela dos dados sem formatação. CPU, memória e disco mostram percentuais e barras de utilização; taxas e volumes usam B/KiB/MiB/GiB. Frações de 0–1 são apresentadas como porcentagem, preservando o valor recebido na tabela.

CPU do servidor é `100 − idle`; CPU de container continua em percentual de um núcleo, podendo exceder 100%. Memória de container só ganha percentual quando existe um limite positivo para a mesma identidade e timestamp. Swap usa capacidade e espaço livre obtidos na mesma janela e grade temporal; capacidade zero ou não verificável não produz percentual. Ausências e lacunas não são convertidas em zero.

As faixas de 80% e 90% são referências visuais, explicitamente separadas das regras de alerta. A classificação é do período consultado e considera todas as séries, mesmo se alguma estiver oculta no desenho. Cobertura incompleta e amostras antigas ficam sinalizadas. Os labels completos continuam disponíveis para investigação.

O período informa quando a janela está fixa. **Trazer para agora** atualiza a janela; **Acompanhar ao vivo (60s)** é opcional e mantém a seleção. A lista de servidores apresenta `host_name` quando disponível ou a identidade `instance` da coleta; não usa o hostname interno do container do exporter como nome principal nem inventa correlações com logs.

## Investigação do tqi-platform

A coleta estava presente no Prometheus e na aplicação autenticada durante esta investigação. Na consulta de 11:01:34 BRT, CPU estava em aproximadamente 35,7%, memória em 45,5% e o filesystem principal em 49%. Foram observados 26 containers associados à identidade de métricas do servidor. A lista destacava o hostname interno do exporter; o Safari também mantinha o bundle anterior até recarregar.

Isso comprova disponibilidade de dados nessa consulta, não prova que nunca houve lacunas. Não houve alteração ou reinício de collector. A capacidade de swap dos dois hosts observados era zero; o indicador legado de 100% não era válido para esses hosts.

## Validação

- 25 testes unitários web passaram.
- 23 testes de navegador existentes passaram, incluindo filtros, troca de recurso, respostas fora de ordem, navegação, recarregamento, mobile e estados de fonte.
- Um teste adicional NOC passou: percentuais, eixos, inspeção por teclado, série oculta sem mascarar capacidade e largura mobile de 390px.
- Teste de atualização automática aprovado: renova a janela a cada 60s, mantém o recurso e interrompe consultas ao pausar.
- Build com `VITE_BASE_PATH=/sentinelops/` aprovado; revisão SRE local aprovada.
- Interface `c723663a3b4c9411e4dac3b10c44c8bae4965ed5` publicada às 14:27:35 UTC. As capturas locais usam fixtures e não constituem evidência de produção.
- Build reproduzido no servidor com fonte e saída idênticas às testadas. Cinco arquivos conferidos por HTTPS a partir do Mac, com MIME correto. Configuração runtime e 23 containers preservados; as duas APIs continuam saudáveis na versão `8b4abdb`.
- Antes da publicação seguinte, o Safari autenticado exibiu os gráficos NOC reais do tqi-platform: CPU 35,69%, memória 45,49%, disco principal 48,96%, taxas em KiB/s e swap sem percentual. Esses valores pertencem à janela encerrada às 11:01:34 BRT. A interface foi depois atualizada com [seletores diretos de recursos](seletores-recursos-2026-09-21.md), cujo aceite visual é registrado separadamente.

Nova consulta autenticada pós-publicação às 14:29:43 UTC confirmou memória em 45,76%, disco principal em 49,19% e 26 containers do tqi-platform. O endpoint auxiliar de capacidade de swap retornou `available` e valor zero.

## Publicação e reversão

O download do GitHub foi rejeitado pelo servidor devido à cadeia de certificados da inspeção TLS. Nenhuma validação TLS ou configuração de confiança foi desativada. O delta público de fonte foi transferido pela sessão SSH autenticada; uma transmissão com hash divergente foi rejeitada antes de aplicar alterações. A transmissão corrigida e a árvore completa foram validadas antes do build isolado em Node 26.9.0.

A publicação instalou os assets antes da troca atômica do índice e preservou o `config.js`. O backup do índice e o registro `published.json` ficam em `/opt/sentinelops/releases/web-noc-c723663/`. O índice anterior continua apto a rollback, com seus assets preservados. O marcador web é `/opt/sentinelops/WEB_DEPLOYED_COMMIT`; o marcador da API permanece separado.

| Artefato | SHA-256 |
|---|---|
| Árvore fonte web | `b45292357783816acdcc1d48b71197a9f93516039ac98b85470c99010c0a96a3` |
| HTML | `50470a03280ce00b68d362fd79df88b6515fc22309d69dcd60a952fffc869859` |
| JavaScript `index-HH0mkuur.js` | `f2be21d96c9c5783ace7a16bd1d1cab9e1cfe8b997cbdfc2cce37184276f668d` |
| CSS `index-BH9KD6_8.css` | `fc773d704781d537c6afebfbce260b75b05378c95949b455a8a0e3f71d434996` |

