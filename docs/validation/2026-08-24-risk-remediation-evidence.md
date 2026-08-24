# Evidência de remediação dos riscos residuais

Data: 2026-08-24.

## Imagens locais imutáveis

`make prepare-images` constrói as imagens próprias, resolve cada uma para seu
ID de conteúdo `sha256:` e gera um override runtime modo `0600`. O Compose sobe
com `pull_policy: never` e pelos IDs, não pelas tags mutáveis. A prova de HA
inspecionou os containers ativos e recusaria qualquer imagem própria sem ID.

O manifesto desses IDs foi assinado e verificado com Cosign 3.0.5. A chave
privada efêmera foi eliminada; manifesto, bundle e chave pública permanecem no
diretório modo `0700` mais recente em `artifacts/release/local-*/`.
O campo `sourceDirty` permanece explícito enquanto a entrega não estiver em um
commit; a assinatura local não atribui alterações do workspace ao `HEAD`.

## HA de processo local

`make prove-ha` confirmou:

- duas réplicas de API e duas de worker;
- borda Nginx com resolução DNS dinâmica e retry do upstream;
- interrupção controlada de uma API;
- convergência em 11 segundos;
- 50 probes consecutivos com sucesso após convergência;
- restauração das duas réplicas;
- imagens próprias ativas referenciadas por SHA256.

Evidência runtime: `artifacts/resilience/ha-20260824T135201Z.json`, modo `0600`.
A proteção cobre falha de processo no laboratório. A perda do host continua
sendo domínio do perfil Kubernetes distribuído.

## HA Kubernetes renderizada

`make prove-ha-manifests` renderizou e validou 26 recursos Kubernetes:

- API/worker/web/gateway com 5/5/3/3 réplicas;
- topology spread `DoNotSchedule` e pod anti-affinity;
- quatro PDBs e HPA de 5 a 30 APIs;
- NetworkPolicies e imagens obrigatórias por digest;
- kubeconform estrito: 26 válidos, zero inválidos;
- Compose aceita somente `development`, `local` ou `test` e recusou
  `production`, `prod`, `PRD` e `staging`.

Evidência: artefato modo `0600` mais recente em
`artifacts/resilience/helm-distributed-*.json`.

## Cold start do Pyroscope

`make prove-resilience` interrompeu o Pyroscope preservando o volume. O guard
detectou a falha, o serviço voltou em 65 segundos dentro do SLO de 240 segundos,
o guard retornou a `healthy` e os perfis dos três mocks permaneceram
consultáveis.

Evidência: `artifacts/resilience/pyroscope-recovery-20260824T134103Z.json`.

## Supply chain e limite externo

`make validate-release` aprovou actionlint, gate de Environment production,
assinatura keyless planejada, promoção por digest e assinatura local. A
evidência registrou corretamente `remote_status=NOT_CONFIGURED` em vez de
simular CI remoto.

O único passo que não pode ser resolvido sem decisão externa é escolher a conta
ou organização GitHub e a visibilidade do novo repositório. Depois dessa
definição, `scripts/prove-remote-ci.sh` exige CI terminal verde no HEAD, reviews
e checks obrigatórios na branch padrão.
