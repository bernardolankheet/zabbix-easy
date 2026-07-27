#!/usr/bin/env bash
# Substitui a versão antiga do Zabbix Easy Report pela versão atual deste repositório.
# Execute NO SERVIDOR, dentro da pasta do projeto (ou após git clone/rsync).
#
# Uso:
#   chmod +x scripts/deploy-server.sh
#   ./scripts/deploy-server.sh
#
# Com Postgres:
#   ./scripts/deploy-server.sh --with-db

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

WITH_DB=false
for arg in "$@"; do
  case "$arg" in
    --with-db) WITH_DB=true ;;
    -h|--help)
      echo "Uso: $0 [--with-db]"
      exit 0
      ;;
  esac
done

echo "==> Zabbix Easy Report — deploy em $(hostname)"
echo "    Diretório: $ROOT"

if ! command -v docker >/dev/null 2>&1; then
  echo "ERRO: Docker não encontrado. Instale Docker antes de continuar."
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

echo "==> Parando containers antigos (nomes comuns)..."
for name in zabbix-easy-report go-zabbix-app zabbix-easy zabbix-easy-app; do
  if docker ps -a --format '{{.Names}}' | grep -qx "$name"; then
    echo "    Removendo container: $name"
    docker rm -f "$name" 2>/dev/null || true
  fi
done

# docker run legado
if docker ps -a --format '{{.Names}}' | grep -qx 'zabbix-easy'; then
  docker rm -f zabbix-easy 2>/dev/null || true
fi

echo "==> Parando stack compose anterior (se existir)..."
$COMPOSE -f docker-compose.yml down 2>/dev/null || true
$COMPOSE -f docker-compose.prod.yml down 2>/dev/null || true

if [[ ! -f .env ]]; then
  echo "==> Criando .env a partir de .env.example..."
  cp .env.example .env
  echo "    ATENÇÃO: edite .env e ajuste ZABBIX_SERVER_HOSTID antes de usar em produção."
fi

echo "==> Build e subida da nova versão..."
if $WITH_DB; then
  grep -q '^DB_HOST=' .env || echo 'DB_HOST=postgres' >> .env
  $COMPOSE -f docker-compose.prod.yml --profile db up --build -d
else
  $COMPOSE -f docker-compose.prod.yml up --build -d
fi

echo "==> Aguardando healthcheck..."
sleep 5
PORT="${APP_PORT:-8080}"
if grep -q '^APP_PORT=' .env 2>/dev/null; then
  PORT="$(grep '^APP_PORT=' .env | cut -d= -f2 | tr -d '"')"
fi

if curl -sf "http://127.0.0.1:${PORT}/api/config" >/dev/null; then
  echo "==> OK — aplicação respondendo em http://$(hostname -I | awk '{print $1}'):${PORT}"
  curl -s "http://127.0.0.1:${PORT}/api/config" | head -c 200
  echo
else
  echo "==> AVISO: healthcheck falhou. Logs:"
  $COMPOSE -f docker-compose.prod.yml logs --tail=40 go-app
  exit 1
fi

echo "==> Deploy concluído. Container: zabbix-easy-report"
$COMPOSE -f docker-compose.prod.yml ps
