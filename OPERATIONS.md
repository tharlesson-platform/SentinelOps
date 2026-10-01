# Operação — SentinelOps

## CI de imagens

- A imagem web usa `apk upgrade` e pacotes sem pins de versão para acompanhar o repositório Alpine da imagem base.
- O build continua sujeito ao scan Trivy com severidades `CRITICAL,HIGH`; vulnerabilidades corrigíveis devem ser resolvidas atualizando a imagem ou os pacotes.
- Rollback: restaurar o commit da mudança do Dockerfile e repetir o build/scan.
- O estágio final do Alloy atualiza libssl3t64/openssl antes de retornar ao usuário não privilegiado.
