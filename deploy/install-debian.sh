#!/usr/bin/env bash
# install-debian.sh — Installation de Prix-Essence sur un LXC Debian (systemd)
#
# À exécuter DANS le terminal du LXC, en root (ou sudo), en une fois :
#   bash <(wget -qO- https://raw.githubusercontent.com/Zephyrell/Prix-Essence/v1.0.0/deploy/install-debian.sh)
#
# Ou manuellement :
#   wget -O /tmp/install-debian.sh https://github.com/Zephyrell/Prix-Essence/releases/download/v1.0.0/prix-essence-linux-amd64 && ...
#   bash /tmp/install-debian.sh <port>   (port par défaut 8080)
#
# La 2e commande ci-dessus n'installe QUE le binaire ; ce script télécharge tout.

set -euo pipefail

APP_NAME="prix-essence"
APP_DIR="/opt/${APP_NAME}"
PORT="${1:-8080}"
DATA_DIR="${APP_DIR}/data"
BIN="$APP_DIR/${APP_NAME}"
USER="prixessence"
RELEASE="https://github.com/Zephyrell/Prix-Essence/releases/download/v1.0.0/"
BINARY_URL="${RELEASE}prix-essence-linux-amd64"

# --- 0. Pré-requis réseau : on doit pouvoir télécharger ---
command -v wget >/dev/null 2>&1 || { echo "ERREUR : wget manquant (apt install wget)." >&2; exit 1; }

echo "==> (${APP_NAME}) Port : ${PORT}"

echo "==> Téléchargement du binaire Linux"
wget -q -O /tmp/${APP_NAME} "${BINARY_URL}" || {
  echo "ERREUR : téléchargement impossible depuis ${BINARY_URL}" >&2;
  echo "Le dépôt est-il public ? Internet du LXC OK ?" >&2;
  echo "Repli : place le binaire dans /tmp/${APP_NAME} et relance." >&2;
  exit 1;
}
chmod +x /tmp/${APP_NAME}
ls -la /tmp/${APP_NAME}

echo "==> Utilisateur système non-root"
if ! id -u "${USER}" >/dev/null 2>&1; then
  adduser --system --no-create-home --home "${APP_DIR}" --group "${USER}"
fi

echo "==> Installation binaire + dossier de données"
mkdir -p "${DATA_DIR}"
chown -R "${USER}:${USER}" "${APP_DIR}"
install -o "${USER}" -g "${USER}" -m 0755 /tmp/${APP_NAME} "${BIN}"

echo "==> Service systemd"
cat > /etc/systemd/system/${APP_NAME}.service <<EOF
[Unit]
Description=${APP_NAME} — prix des carburants en France
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${USER}
Group=${USER}
WorkingDirectory=${APP_DIR}
Environment=DB_PATH=${DATA_DIR}/prix-essence.db
# Écoute sur toutes les interfaces (joignable depuis le Mac via WireGuard).
# En face d'un reverse proxy public on mettrait 127.0.0.1 à la place.
Environment=LISTEN=0.0.0.0:${PORT}
Environment=REFRESH_ON_START=true
ExecStart=${BIN}
Restart=on-failure
RestartSec=5
Nice=5

# Sécurité de base du service
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload

echo "==> Démarrage + activation au boot"
systemctl enable --now ${APP_NAME}
systemctl restart ${APP_NAME}

sleep 2
echo ""
echo "==> État du service :"
systemctl --no-pager status ${APP_NAME} --lines=6
echo ""
echo "==> Test local :"
if curl -fsS "http://127.0.0.1:${PORT}/api/status" >/dev/null 2>&1 || \
   wget -qO- "http://127.0.0.1:${PORT}/api/status" >/dev/null 2>&1; then
  echo "   ✔ L'app répond sur http://127.0.0.1:${PORT}"
else
  echo "   ! L'app ne répond pas encore (import des données en cours au 1er démarrage ?)."
  echo "     Recontrôle dans quelques secondes : journalctl -u ${APP_NAME} -f"
fi
echo ""
echo "Accès depuis ton Mac via WireGuard :  http://192.168.1.11:${PORT}"
echo "Journal :  journalctl -u ${APP_NAME} -f"
