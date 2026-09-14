# Perfil de VM Linux

Este perfil mantém o estado pesado do SentinelOps fora do disco do sistema:

- `/srv/sentinelops`: volume dedicado de dados;
- `/srv/sentinelops/docker`: Docker Engine;
- `/srv/sentinelops/containerd`: imagens, snapshots e conteúdo do containerd;
- `/opt/sentinelops`: release, configurações e scripts da plataforma.

Instale as unidades `srv-sentinelops.mount` e `var-lib-containerd.mount` antes
de iniciar Docker. A segunda é um bind mount e requer que o estado existente do
containerd seja copiado para o volume com Docker e containerd parados.

O proxy interno e seus limites de exposição estão em `traefik/README.md`.
