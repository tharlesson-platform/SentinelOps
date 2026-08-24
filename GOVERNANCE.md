# Governança

O SentinelOps adota governança orientada a plataforma/SRE para tornar mudanças
auditáveis, reversíveis e baseadas em evidência.

## Papéis

- **Mantenedor:** define roadmap, revisa mudanças e administra o repositório.
- **Platform/SRE:** mantém bootstrap, deploy, telemetria, SLOs e runbooks.
- **Security reviewer:** revisa identidade, secrets, supply chain e exposição.
- **App team:** mantém instrumentação, catálogo, cenários e ownership do serviço.
- **Operação:** responde alertas e executa procedimentos aprovados.

Uma pessoa pode acumular papéis, mas mudanças críticas devem registrar uma
revisão independente sempre que a equipe permitir.

## Modelo de decisão

1. Proposta por issue, ADR ou Pull Request.
2. Mudança focada com risco, evidência e rollback.
3. Validações automáticas no SHA atual.
4. Aprovação de CODEOWNERS em caminhos críticos.
5. Merge em `main` protegida.
6. Release assinada e promoção declarativa por GitOps.
7. Validação de runtime e observabilidade no ambiente alvo.

Decisões reversíveis podem ocorrer no Pull Request. Decisões que alteram
contratos, confiança, persistência, tenancy ou arquitetura exigem ADR.

## Fontes de verdade

- GitHub: fonte do código, configuração desejada e trilha de mudança.
- Registry: artefatos imutáveis por digest, SBOM e assinatura.
- Argo CD: reconciliação declarativa do cluster.
- Cluster e backends: estado observado, comprovado por readiness, SLOs e dados.

## Releases e produção

Tags semânticas disparam o workflow de release. Produção exige aprovação,
checks terminais no SHA, imagens assinadas, secrets externos, IdP homologado,
backup/restore e rollback ensaiados. Evidência local não substitui esses gates.

## Conflitos e segurança

Conflitos técnicos são resolvidos com evidência e ADR. Vulnerabilidades seguem
[SECURITY.md](SECURITY.md) e têm prioridade sobre roadmap público.
