#!/usr/bin/env bash
# Deploy sem build — usa imagem já exportada (servidor sem Docker Hub).
#
# Uso (no servidor):
#   scp zabbix-easy-report.tar.gz root@SERVIDOR:/opt/zabbix-easy-report/
#   cd /opt/zabbix-easy-report
#   chmod +x scripts/deploy-offline.sh
#   ./scripts/deploy-offline.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ARCHIVE="${1:-zabbix-easy-report.tar.gz}"
IMAGE="zabbix-easy-report:local"

if ! command -v docker >/dev/null 2>&1; then
  echo "ERRO: Docker não encontrado."
  exit 1
fi

COMPOSE="docker compose"
if ! docker compose version >/dev/null 2>&1; then
  if command -v docker-compose >/dev/null 2>&1; then
    COMPOSE="docker-compose"
  else
    echo "ERRO: docker compose não disponível."
    exit 1
  fi
fi

if [[ ! -f "$ARCHIVE" ]]; then
  echo "ERRO: arquivo não encontrado: $ROOT/$ARCHIVE"
  echo "Envie a imagem da máquina de build:"
  echo "  scp zabbix-easy-report.tar.gz root@$(hostname -I | awk '{print $1}'):$ROOT/"
  exit 1
fi

echo "==> Zabbix Easy Report — deploy offline em $(hostname)"

echo "==> Parando containers antigos..."
for name in zabbix-easy-report go-zabbix-app zabbix-easy zabbix-easy-app; do
  if docker ps -a --format '{{.Names}}' | grep -qx "$name"; then
    docker rm -f "$name" 2>/dev/null || true
  fi
done
$COMPOSE -f docker-compose.yml down 2>/dev/null || true
$COMPOSE -f docker-compose.prod.yml down 2>/dev/null || true

if [[ ! -f .env ]]; then
  cp .env.example .env
  echo "ATENÇÃO: .env criado — ajuste ZABBIX_SERVER_HOSTID se necessário."
fi

echo "==> Carregando imagem de $ARCHIVE ..."
docker load < "$ARCHIVE"

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "ERRO: imagem $IMAGE não encontrada após docker load."
  exit 1
fi

echo "==> Subindo stack (sem build)..."
$COMPOSE -f docker-compose.prod.yml up -d --no-build

echo "==> Aguardando..."
sleep 5
PORT="8080"
if grep -q '^APP_PORT=' .env 2>/dev/null; then
  PORT="$(grep '^APP_PORT=' .env | cut -d= -f2 | tr -d '"')"
fi

if curl -sf "http://127.0.0.1:${PORT}/api/config" >/dev/null; then
  echo "==> OK — http://$(hostname -I | awk '{print $1}'):${PORT}"
  curl -s "http://127.0.0.1:${PORT}/api/config"
  echo
else
  echo "==> AVISO: healthcheck falhou. Logs:"
  $COMPOSE -f docker-compose.prod.yml logs --tail=40 go-app
  exit 1
fi

$COMPOSE -f docker-compose.prod.yml ps
