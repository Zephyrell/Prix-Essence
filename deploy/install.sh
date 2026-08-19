#!/bin/sh
# install.sh — installé sur le LXC Proxmox (Alpine)
#
# Usage :
#   1. Envoyer le binaire et ce script sur le LXC (scripts/deploy.sh le fait).
#   2. Exécuter :  sh /tmp/install.sh <port>   (port par défaut : 8080)
#
# Pré-requis LXC Alpine :  apk add bash curl ca-certificates tzdata
# Des paquets optionnels :  apk add caddy   (si tu veux le reverse proxy HTTPS)

set -e

APP_NAME="prix-essence"
APP_DIR="/opt/${APP_NAME}"
PORT="${1:-8080}"
DATA_DIR="${APP_DIR}/data"
BIN="$APP_DIR/${APP_NAME}"
USER="prixessence"

# --- Vérification binaire (copié précédemment dans /tmp) ---
if [ ! -f /tmp/${APP_NAME} ]; then
  echo "ERREUR : le binaire manque dans /tmp/${APP_NAME}. Copie-le d'abord." >&2
  exit 1
fi

echo "==> Création de l'utilisateur système (non-root)"
if ! id -u "${USER}" >/dev/null 2>&1; then
  adduser -D -H -s /bin/sh "${USER}"
fi

echo "==> Installation du binaire et des données"
mkdir -p "${DATA_DIR}"
chown -R "${USER}:${USER}" "${APP_DIR}"
install -m 0755 /tmp/${APP_NAME} "${BIN}"

echo "==> Configuration du service (OpenRC)"
cat > /etc/init.d/${APP_NAME} <<EOF
#!/sbin/openrc-run
description="${APP_NAME} : prix des carburants"

depend() {
    need net
}

: \${DB_PATH:="${DATA_DIR}/prix-essence.db"}
: \${LISTEN:="127.0.0.1:${PORT}"}
: \${REFRESH_ON_START:="true"}

command="${BIN}"
command_args=""
command_user="${USER}"
command_background="false"
pidfile="/run/${APP_NAME}.pid"
output_log="/var/log/${APP_NAME}.log"
error_log="/var/log/${APP_NAME}.log"
EOF
chmod +x /etc/init.d/${APP_NAME}

echo "==> Active le service au démarrage"
rc-update add ${APP_NAME} default

echo "==> Le service est prêt. Démarre-le avec :"
echo "    rc-service ${APP_NAME} start"
echo "    # ou via le script deploy.sh qui le fait automatiquement."
echo ""
echo "Pour le reverse proxy HTTPS : copie deploy/Caddyfile et lance  rc-service caddy start"
