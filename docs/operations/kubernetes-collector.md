# Collector Kubernetes read-only

`apps/kubeinventory` transforma objetos Kubernetes em snapshot para o agente
SentinelOps sem usar `secrets`, exec, logs de container ou qualquer API de
escrita. Cada execução exige um contexto Kubernetes e uma lista finita de
namespaces; não existe opção para descobrir todos os namespaces por acidente.

## Instalação da permissão

1. Crie previamente o namespace `sentinelops-observability` pelo processo de
plataforma aprovado. O collector não cria namespaces.
2. Revise e aplique o RBAC cluster read-only:

```sh
kubectl apply -f deploy/agents/kubernetes/rbac-cluster.yaml
```

3. Copie `scope.example.json` para um arquivo local `scope.json`, informe
somente o contexto e namespaces autorizados, e gere os RoleBindings fechados:

```sh
python3 scripts/render-kubernetes-rbac.py \
  --scope /etc/sentinelops/kubernetes-scope.json \
  --output /tmp/sentinelops-rbac-namespaces.yaml
kubectl apply -f /tmp/sentinelops-rbac-namespaces.yaml
```

O ClusterRole contém somente `get,list` de `nodes`, `namespaces`, pods,
services, PVCs e workloads. Não contém `secrets`, `configmaps`, `exec`,
`watch`, `create`, `patch`, `update` ou `delete`. As permissões de workload só
recebem RoleBinding nos namespaces declarados.

## Coleta e publicação

Forneça ao processo `kubectl` uma identidade de ServiceAccount projetada ou
contexto federado já autorizado. Execute:

```sh
./scripts/discover-kubernetes-inventory.sh \
  --config /etc/sentinelops/kubernetes-scope.json \
  --output /var/lib/sentinelops-agent/inventory.json
```

Antes de listar objetos, o collector confirma acesso `list` em cada recurso
necessário e exige negação de `get secrets` e `create deployments`. Ele usa o
UID de `kube-system` como âncora estável do cluster e UID de cada objeto para
gerar `assetId`; não importa labels arbitrários nem conteúdo de pod. Falha de
RBAC, contexto, timeout, objeto sem UID ou resultado acima de mil itens não
substitui o snapshot anterior. Combine com `SENTINEL_INVENTORY_MAX_AGE` no
agente para que arquivo antigo não seja republicado como novo.

Este corte ainda não coleta eventos, métricas kubelet/kube-state-metrics,
logs, traces, OOM, quotas, ingress ou métricas de bancos. Não habilite esses
itens no dashboard como se já estivessem disponíveis.

## Rollout e rollback

Comece com um namespace de homologação e compare pod, node e workload com a API
nativa. Teste explicitamente que `kubectl auth can-i get secrets -n NAMESPACE`
e `kubectl auth can-i create deployments -n NAMESPACE` retornam `no`. Para
rollback, interrompa o job/agent e remova os RoleBindings renderizados e o
ClusterRoleBinding; não altere workloads do time observado.
