#!/usr/bin/env bash
# Build local e exporta imagem para servidor sem acesso ao Docker Hub.
#
# Uso (na máquina de desenvolvimento, com internet):
#   chmod +x scripts/export-image.sh
#   ./scripts/export-image.sh
#
# Depois envie ao servidor:
#   scp zabbix-easy-report.tar.gz root@SERVIDOR:/opt/zabbix-easy-report/

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

IMAGE="zabbix-easy-report:local"
OUT="${1:-zabbix-easy-report.tar.gz}"

echo "==> Build da imagem $IMAGE"
docker build -t "$IMAGE" ./app

echo "==> Exportando para $OUT"
docker save "$IMAGE" | gzip > "$OUT"

echo "==> OK — $(du -h "$OUT" | cut -f1) em $ROOT/$OUT"
echo
echo "Próximo passo no servidor:"
echo "  cd /opt/zabbix-easy-report"
echo "  ./scripts/deploy-offline.sh"
