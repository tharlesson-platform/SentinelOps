# PostgreSQL: roles, migração e isolamento por tenant

O SentinelOps usa duas identidades de banco diferentes:

- `sentinel_migration`: owner usado somente pelo Job de migração;
- `sentinel_app`: role não-superusuária usada continuamente por API e worker.

Nunca configure `database-url` com owner, `BYPASSRLS` ou superusuário. O pool
reseta `app.organization_id` em toda aquisição de conexão; contexto ausente ou
UUID inválido enxerga zero linhas. As 41 tabelas tenant-scoped, diretas e
filhas, usam `ENABLE/FORCE ROW LEVEL SECURITY`. O tenant da migração (`*`) só é
criado dentro do pacote `database`, não vem de request, claim ou header e só é
aceito quando a conexão atual é dona da função de migração. Mesmo que a role de
runtime tente executar `set_config(..., '*', ...)`, as policies continuam
negando acesso.

## Bootstrap das roles

Crie o banco com uma identidade owner, gere uma senha independente para a role
de runtime e execute:

```bash
export DATABASE_MIGRATION_URL='postgres://sentinel_migration:...@db.example:5432/sentinel?sslmode=verify-full'
export APP_DATABASE_USER=sentinel_app
export APP_DATABASE_PASSWORD='senha-aleatoria-do-secret-store'
./scripts/bootstrap-postgres-roles.sh
```

O script é idempotente, recusa nomes de role inseguros, remove `CREATE` público
do schema e concede somente conexão, uso do schema, DML, sequences e funções.
Ele também define privilégios padrão para objetos futuros. Não grave essas
variáveis em shell history; em automação, injete-as pelo secret store.

Depois, publique no secret store:

- `migration-database-url`: URL do owner, consumida somente pelo Job hook;
- `database-url`: URL de `sentinel_app`, consumida por API e worker.

O Job Helm/Argo executa antes do rollout, usa imagem própria por digest e tem
NetworkPolicy apenas para DNS e PostgreSQL. Os Deployments não recebem a URL de
migração.

## Teste negativo obrigatório

O CI sobe PostgreSQL real, provisiona as duas roles, roda a migração e executa
`TestPostgresRowLevelTenantIsolation`. O teste falha se a role de runtime for
superusuária, se contexto vazio retornar linhas, se A enxergar B ou se um
insert direto/filho puder referenciar o tenant B.

No ambiente alvo, repita a evidência com uma base descartável/restaurada e
confirme:

```sql
SELECT current_user, rolsuper, rolbypassrls
FROM pg_roles WHERE rolname = current_user;

SELECT count(*)
FROM pg_class
WHERE relrowsecurity AND relforcerowsecurity;
```

Esperado para runtime: `rolsuper=false`, `rolbypassrls=false` e 41 tabelas.
Não altere `app.organization_id` manualmente em sessões da aplicação.
