# Instalação

## Requisitos

- Docker

---

## Opção 1 — Docker Run (mais simples)

Ideal para uso rápido, sem precisar clonar o repositório.

```bash
docker run -d \
  --name zabbix-easy \
  -p 8080:8080 \
  -e MAX_CCONCURRENT=10 \
  -e ZABBIX_SERVER_HOSTID=10084 \
  -e CHECKTRENDTIME=15d \
  bernardolankheet/zabbix-easy:latest
```

> **Como encontrar o `ZABBIX_SERVER_HOSTID`:** Acesse o frontend do Zabbix → **Data Collection** → pesquise pelo host "Zabbix Server" → abra o host e verifique o ID na URL. O valor padrão é `10084`.

Acesse a interface em `http://localhost:8080`.

---

## Opção 2 — Docker Compose (sem banco de dados)

Relatórios ficam disponíveis apenas na sessão atual. Ao reiniciar o container, os relatórios anteriores são perdidos.

```bash
git clone https://github.com/bernardolankheet/zabbix-easy.git
cd zabbix-easy
docker compose up --build -d
```

Acesse a interface em `http://localhost:8080`.

---

## Opção 3 — Docker Compose (com banco de dados PostgreSQL)

Persiste todos os relatórios gerados. Permite reabrir, comparar e excluir relatórios anteriores.

### Passo 1 — Habilitar as variáveis de banco no `docker-compose.yml`

Descomente o bloco de variáveis `DB_*` no serviço `go-app`:

```yaml
environment:
  - DB_HOST=postgres
  - DB_PORT=5432
  - DB_USER=postgres
  - DB_PASSWORD=postgres
  - DB_NAME=zabbix_report
```

### Passo 2 — Subir com o profile `db`

```bash
docker compose --profile db up --build -d
```

O profile `db` é necessário pois o serviço `postgres` **só é iniciado quando explicitamente solicitado**.

Acesse a interface em `http://localhost:8080`.

---

## Variáveis de ambiente principais

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `ZABBIX_SERVER_HOSTID` | `10084` | ID do host "Zabbix Server" no frontend do Zabbix. Usado para coleta de métricas de performance. |
| `CHECKTRENDTIME` | `30d` | Janela de tempo para análise de trends. Aceita `d` (dias), `h` (horas). Ex: `15d`, `24h`. |
| `MAX_CCONCURRENT` | `4` | Goroutines paralelas para chamadas à API do Zabbix. Reduzir para `2`–`3` se o Zabbix ficar lento. |
| `API_TIMEOUT_SECONDS` | `60` | Timeout em segundos por requisição. Aumentar para `90`–`120` em ambientes lentos. |
| `APP_DEBUG` | _(vazio)_ | `true` para logs detalhados de cada requisição à API. |
| `DB_HOST` | _(vazio)_ | Host do PostgreSQL. **Se vazio, persistência desativada.** |

