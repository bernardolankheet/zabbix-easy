---
title: "Installation"
lang: en_US
---

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

> **How to find `ZABBIX_SERVER_HOSTID`:** In the Zabbix frontend go to **Data Collection**, search for the host "Zabbix Server", open its page and check the ID in the URL. The default is `10084`.

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

## Option 4 — Helm (Kubernetes)

Use the chart included in `helm/zabbix-easy`. Examples below install the chart from the local checkout into the current cluster.

Basic install (local chart):

```bash
helm upgrade --install zabbix-easy ./helm/zabbix-easy \
  --namespace zabbix-easy --create-namespace
```

Set image and application env via `--set`:

```bash
helm upgrade --install zabbix-easy ./helm/zabbix-easy \
  -n zabbix-easy --create-namespace \
  --set image.tag=latest \
  --set env.ZABBIX_SERVER_HOSTID=10084 \
  --set env.CHECKTRENDTIME=15d
```

Use a custom `values.yaml` (recommended for production):

```bash
# create my-values.yaml with overrides (ex: ingress host, postgres.enabled: false, etc.)
helm upgrade --install zabbix-easy ./helm/zabbix-easy \
  -n zabbix-easy --create-namespace -f my-values.yaml
```

Useful examples:

- Disable internal PostgreSQL and use external DB:

```yaml
postgres:
  enabled: false

  DB_HOST: my-postgres-host
  DB_USER: myuser
  DB_PASSWORD: mypass
```

- Adjust Ingress (edit `my-values.yaml` to configure `ingress.rules`/`host`):

```yaml

  enabled: true
  rules:
    - host: zabbix-easy.exemple.local
      path: /
  tlsenabled: true
```

After install, reach the service via Ingress/LoadBalancer or use port-forward:

```bash
kubectl port-forward svc/zabbix-easy -n zabbix-easy 8080:8080
# visit http://localhost:8080
```

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

