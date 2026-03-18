# Docker

## Execution Modes

Zabbix Easy supports two modes: **without database** (default) and **with database** for report persistence.
The `DB_HOST` environment variable controls this: if empty, no database is needed; the "Saved Reports" card is hidden and reports are kept only for the current session (in-memory).

```
var dbEnabled = os.Getenv("DB_HOST") != ""
```

---

## Mode 1 — Without database (default)

Ideal for quick evaluation.

```bash
docker compose up --build -d
```

- Only the `go-app` service starts.
- Reports are available **only in the current session** (in-memory).
- The **Saved Reports** card is **automatically hidden**.
- Restarting the container loses all reports.

---

## Mode 2 — With PostgreSQL

Persists all reports. Allows reopening, comparing and deleting past reports.

### Step 1 — Enable database variables in `docker-compose.yml`

Uncomment the `DB_*` block under `go-app`:

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

---

## Environment Variables Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `ZABBIX_SERVER_HOSTID` | 10048 | Zabbix Server host ID for performance data collection. |
| `CHECKTRENDTIME` | `30d` | Trend analysis window. Accepts `d`, `h`, `m`. |
| `MAX_CCONCURRENT` | `4` | Parallel goroutines for Zabbix API calls. |
| `API_TIMEOUT_SECONDS` | `60` | Per-request HTTP timeout in seconds. |
| `APP_DEBUG` | _(empty)_ | `true` for verbose API request logs. |
| `DB_HOST` | _(empty)_ | PostgreSQL host. **If empty, persistence is disabled.** |

---

## Useful Commands

```bash
# Start without database
docker compose up -d

# Start with database
docker compose --profile db up --build -d

# View app logs
docker logs -f go-zabbix-app

# Stop everything (preserves data volume)
docker compose --profile db down

# Stop and remove data volume
docker compose --profile db down -v
```
