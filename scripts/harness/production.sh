#!/bin/sh
set -eu
case "$1" in *[!A-Za-z0-9._-]*|'') echo 'FAIL: TARGET deve ser identificador sem URL' >&2; exit 1;; esac
echo "BLOCKED: alvo $1 sem autorização/evidência. Necessário: identidade, escopo, conectividade, janela e rollback." >&2
exit 2
