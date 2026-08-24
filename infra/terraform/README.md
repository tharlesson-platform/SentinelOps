# Terraform AWS por ambiente

Os roots `dev`, `stg` e `prod` compõem RDS PostgreSQL e bucket de artefatos; não
são mais esqueletos. Cada root exige VPC/subnets privadas, security group
restritivo, KMS e bucket globalmente único fornecidos pelo operador.

Controles incluídos: RDS privado, senha master gerenciada, KMS, TLS forçado,
Multi-AZ em STG/PRD, PITR, deletion protection, final snapshot, Enhanced
Monitoring, Performance Insights, logs criptografados/retidos e parâmetros de
auditoria. S3 usa KMS, versionamento, ownership enforced, public-access block,
TLS obrigatório, lifecycle e `prevent_destroy`.
O plano recusa subnets com auto-assign de IP público ou sem duas AZs, security
groups que exponham algo além de TCP/5432 ou CIDRs globais, tags obrigatórias
ausentes e uploads S3 que tentem substituir o KMS definido por criptografia
mais fraca ou outra chave.

## Fluxo faseado

```bash
cd infra/terraform/environments/prod
cp backend.hcl.example backend.hcl
cp terraform.tfvars.example terraform.tfvars
# substitua todos os placeholders e confirme conta/região com aws sts
terraform init -backend-config=backend.hcl
terraform fmt -check
terraform validate
terraform plan -out=plan.tfplan
terraform show -no-color plan.tfplan > plan.txt
```

Revise conta, região, recursos substituídos, IAM, SG, KMS e custos. `apply` não
é automático no repositório e requer aprovação humana no ambiente alvo:

```bash
terraform apply plan.tfplan
```

Depois, recupere a URL owner pelo secret gerenciado, execute o bootstrap de
roles descrito em `docs/operations/database-tenancy.md` e publique as duas URLs
no secret store. Um `validate` local não prova plano cloud, conectividade ou
PITR; anexe plan, apply, teste de restore e evidência Multi-AZ ao change.
