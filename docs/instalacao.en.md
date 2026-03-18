# Installation

## Requirements

- Docker

---

## Option 1 — Docker Run (quickest)

Ideal for quick evaluation without cloning the repository.

```bash
docker run -d \
  --name zabbix-easy \
  -p 8080:8080 \
  -e MAX_CCONCURRENT=10 \
  -e ZABBIX_SERVER_HOSTID=10084 \
  -e CHECKTRENDTIME=15d \
  bernardolankheet/zabbix-easy:latest
```

> **How to find `ZABBIX_SERVER_HOSTID`:** Go to Zabbix frontend → **Data Collection** → search for host "Zabbix Server" → open the host and check the ID in the URL. Default is `10084`.

Access the UI at `http://localhost:8080`.

---

## Option 2 — Docker Compose (no database)

Reports are available only in the current session. Restarting the container loses all reports.

```bash
git clone https://github.com/bernardolankheet/zabbix-easy.git
cd zabbix-easy
docker compose up --build -d
```

Access the UI at `http://localhost:8080`.

---

## Option 3 — Docker Compose (with PostgreSQL)

Persists all generated reports. Allows reopening, comparing and deleting past reports.

### Step 1 — Enable database variables in `docker-compose.yml`

Uncomment the `DB_*` block under the `go-app` service:

```yaml
environment:
  - DB_HOST=postgres
  - DB_PORT=5432
  - DB_USER=postgres
  - DB_PASSWORD=postgres
  - DB_NAME=zabbix_report
```

### Step 2 — Start with the `db` profile

```bash
docker compose --profile db up --build -d
```

Access the UI at `http://localhost:8080`.

---

## Main environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ZABBIX_SERVER_HOSTID` | `10084` | Zabbix Server host ID. Used for performance metric collection. |
| `CHECKTRENDTIME` | `30d` | Time window for trend analysis. Accepts `d` (days), `h` (hours). |
| `MAX_CCONCURRENT` | `4` | Parallel goroutines for Zabbix API calls. Lower to `2`–`3` if Zabbix is slow. |
| `API_TIMEOUT_SECONDS` | `60` | Per-request timeout in seconds. Increase to `90`–`120` for slow environments. |
| `APP_DEBUG` | _(empty)_ | `true` to enable verbose API request logs. |
| `DB_HOST` | _(empty)_ | PostgreSQL host. **If empty, persistence is disabled.** |
