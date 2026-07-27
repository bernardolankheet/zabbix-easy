#!/usr/bin/env bash
# Atualiza x-oc-grafana-prd com a versão local (build offline).
# Execute NA SUA MÁQUINA (vai pedir senha SSH):
#   chmod +x scripts/update-production.sh
#   ./scripts/update-production.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SERVER="${DEPLOY_SERVER:-root@x-oc-grafana-prd}"
REMOTE_DIR="${DEPLOY_DIR:-/opt/zabbix-easy-report}"

echo "==> Servidor: $SERVER"
echo "==> Destino:  $REMOTE_DIR"
echo

echo "==> [1/4] Build e export da imagem Docker..."
chmod +x scripts/export-image.sh
./scripts/export-image.sh

echo "==> [2/4] Enviando código (rsync)..."
rsync -avz --delete \
  --exclude '.git' \
  --exclude 'app/app' \
  --exclude 'app/go-zabbix-app' \
  --exclude 'app/vendor' \
  --exclude 'zabbix-easy-report.tar.gz' \
  ./ "${SERVER}:${REMOTE_DIR}/"

echo "==> [3/4] Enviando imagem (~8 MB)..."
scp zabbix-easy-report.tar.gz "${SERVER}:${REMOTE_DIR}/"

echo "==> [4/4] Deploy offline no servidor..."
ssh "${SERVER}" "cd ${REMOTE_DIR} && chmod +x scripts/*.sh && ./scripts/deploy-offline.sh"

echo
echo "==> Concluído. Acesse: http://10.3.32.207:8080"
echo "    (Ctrl+Shift+R no navegador para limpar cache)"
