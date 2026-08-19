#!/usr/bin/env bash
# deploy.sh — Archive et pousse le binaire Linux + scripts vers le LXC Proxmox
#
# Pré-requis :
#   - La cible est définie par LXC_HOST / LXC_PORT (fabriquée par Makefile ou .env)
#   - ssh/scp fonctionnent (souvent via clé publique)
#   - Le binaire Linux a été compilé (make build-linux)
set -euo pipefail

: "${LXC_HOST:=root@192.168.1.100}"
: "${LXC_PORT:=22}"
: "${BINARY:=bin/prix-essence-linux-amd64}"
: "${REMOTE_TMP:=/tmp}"

if [ ! -f "${BINARY}" ]; then
  echo "ERREUR : binaire introuvable '${BINARY}'. Lance  make build-linux  d'abord." >&2
  exit 1
fi

echo "==> Copie du binaire et des scripts vers ${LXC_HOST}"
scp -P "${LXC_PORT}" "${BINARY}" "${LXC_HOST}:${REMOTE_TMP}/prix-essence"
scp -P "${LXC_PORT}" deploy/install.sh "${LXC_HOST}:${REMOTE_TMP}/install.sh"
scp -P "${LXC_PORT}" deploy/Caddyfile "${LXC_HOST}:${REMOTE_TMP}/Caddyfile"

echo "==> Installation du service sur le LXC (port 8080)"
ssh -p "${LXC_PORT}" "${LXC_HOST}" \
  'sh /tmp/install.sh 8080 && rc-service prix-essence restart || true'

echo "==> Terminé. Statut :"
ssh -p "${LXC_PORT}" "${LXC_HOST}" 'rc-service prix-essence status || true'

echo "OK."
