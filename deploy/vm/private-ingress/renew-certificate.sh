#!/usr/bin/env bash
# DNS-01: não exige abrir HTTP público nem publicar um registro A público.
set -euo pipefail

readonly DOMAIN=sentinelops.tqi.com.br
readonly EMAIL=platform@tqi.com.br
readonly ACME_DIR=/opt/sentinelops/private-ingress/acme
readonly CERT_DIR=/opt/sentinelops/private-ingress/certs
readonly PROXY_CONTAINER=sentinelops-private-ingress
readonly LEGO_IMAGE=goacme/lego@sha256:ac04a7aaac0270ca2c32f1e79b157087d763e78c4473551c6e093070614536e2

exec 9>/run/lock/sentinelops-certificate.lock
flock -n 9 || exit 0
install -d -m 0700 "${ACME_DIR}" "${CERT_DIR}"

docker run --rm --network host \
  --env-file /opt/sentinelops/private-ingress/secrets/azure-dns.env \
  -e AZURE_CLIENT_SECRET_FILE=/run/secrets/azure-client-secret \
  -e AZURE_TTL=60 \
  -e AZURE_POLLING_INTERVAL=2 \
  -e AZURE_PROPAGATION_TIMEOUT=180 \
  -e LEGO_LOG_FORMAT=text \
  -v "${ACME_DIR}:/var/lib/lego" \
  -v /opt/sentinelops/private-ingress/secrets/azure-client-secret:/run/secrets/azure-client-secret:ro \
  "${LEGO_IMAGE}" run \
  --server letsencrypt --accept-tos --email "${EMAIL}" \
  --dns azuredns --dns.resolvers 1.1.1.1:53 \
  --domains "${DOMAIN}" --path /var/lib/lego \
  --renew-days 30 --no-random-sleep

readonly SOURCE_CERT="${ACME_DIR}/certificates/${DOMAIN}.crt"
readonly SOURCE_KEY="${ACME_DIR}/certificates/${DOMAIN}.key"
test -s "${SOURCE_CERT}"
test -s "${SOURCE_KEY}"
openssl x509 -in "${SOURCE_CERT}" -noout -checkend 2592000
openssl x509 -in "${SOURCE_CERT}" -noout -ext subjectAltName | grep -Fq "DNS:${DOMAIN}"
cert_public="$(openssl x509 -in "${SOURCE_CERT}" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1)"
key_public="$(openssl pkey -in "${SOURCE_KEY}" -pubout -outform DER | sha256sum | cut -d' ' -f1)"
test "${cert_public}" = "${key_public}"

cert_tmp="$(mktemp "${CERT_DIR}/.fullchain.XXXXXX")"
key_tmp="$(mktemp "${CERT_DIR}/.privkey.XXXXXX")"
trap 'rm -f "${cert_tmp:-}" "${key_tmp:-}"' EXIT
install -m 0644 "${SOURCE_CERT}" "${cert_tmp}"
install -m 0600 "${SOURCE_KEY}" "${key_tmp}"
mv -f "${cert_tmp}" "${CERT_DIR}/fullchain.pem"
mv -f "${key_tmp}" "${CERT_DIR}/privkey.pem"

# Permite emitir/renovar antes de ativar as rotas. Certificados devem ser
# montados pelo diretório inteiro, para o reload enxergar arquivos substituídos.
if docker inspect "${PROXY_CONTAINER}" --format '{{.State.Running}}' 2>/dev/null | grep -qx true; then
  docker exec "${PROXY_CONTAINER}" nginx -t -c /etc/nginx/private/nginx.conf
  docker exec "${PROXY_CONTAINER}" nginx -s reload -c /etc/nginx/private/nginx.conf
  curl --fail --silent --show-error --max-time 15 \
    --resolve "${DOMAIN}:19443:127.0.0.1" \
    "https://${DOMAIN}:19443/ingress-healthz" >/dev/null
fi
openssl x509 -in "${CERT_DIR}/fullchain.pem" -noout -subject -issuer -dates
