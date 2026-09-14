# MCP SentinelOps

O binario `sentinelops-mcp` fornece um servidor MCP por `stdio` para consultas de inventário. Ele não abre porta HTTP, não se conecta ao PostgreSQL e não possui ferramentas mutáveis. Cada chamada passa pela API SentinelOps com o token configurado, preservando tenant, RBAC, auditoria e limites da API.

## Pré-requisitos

- API SentinelOps alcançável por HTTPS com cadeia de certificados confiável. HTTP é aceito somente para `localhost`/loopback em desenvolvimento.
- Token de curta duração ou token de conta de serviço com apenas `asset:read`.
- `SENTINEL_API_URL` e `SENTINEL_API_TOKEN` injetados pelo ambiente/gerenciador de segredos do cliente MCP; nunca em repositório, prompt ou log.
- Para gateway que exige mTLS, caminhos de certificado CA, certificado cliente e chave cliente injetados juntos por `SENTINEL_API_CA_FILE`, `SENTINEL_API_CLIENT_CERT_FILE` e `SENTINEL_API_CLIENT_KEY_FILE`. Não há opção para ignorar validação TLS.

## Ferramentas disponíveis

| Ferramenta | Efeito | Limites |
| --- | --- | --- |
| `assets_search` | Pesquisa o catálogo visível ao token | até 100 itens, cursor opaco e termo de até 128 caracteres |
| `asset_get` | Obtém um ativo por `assetId` | somente formato seguro e escopo do token |

Erros de API são devolvidos como erro MCP sem incluir token, URL privada, resposta HTTP ou detalhes internos. Uma ausência de ativo e uma negação de autorização são intencionalmente indistinguíveis para o cliente.

## Configuração do cliente

Use [`.codex/config.toml.example`](../../.codex/config.toml.example) como referência. O binário pode ser compilado em ambiente controlado com:

```bash
go build -o sentinelops-mcp ./apps/mcp
```

Antes de habilitar em homologação ou produção, valide o fluxo com um token dedicado, escopo mínimo e uma organização de teste. A configuração local não é evidência de homologação nem autorização de produção.

O mesmo contrato paginado pode ser inspecionado fora do MCP por `sentinelctl asset search --query "nome" --limit 25`; a resposta inclui o cursor a ser usado na próxima página.
