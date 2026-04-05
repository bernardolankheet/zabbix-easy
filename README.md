# Zabbix Easy Report

Zabbix Easy is an open-source tool that analyzes a Zabbix environment via the Zabbix API and generates a concise HealthCheck report with actionable recommendations to improve performance, reliability and maintenance.

Compatibility: 
- 6.0
- 6.4
- 7.0
- 7.2
- 7.4
- 8.0 (experimental)

Quick summary
- Backend language: Go
- Frontend: HTML/CSS/JS (server-generated)
- Documentation: MkDocs (in the `docs/` folder)

Key components
- `app/cmd/app` — Go backend that collects data via the Zabbix API and renders the HTML report
- `app/web` — static assets (templates, i18n, CSS, JS)
- `docs/` — project documentation (MkDocs)
- `app/internal/collector` — typed Zabbix API collectors centralizing JSON-RPC transport and parsing.

Key helpers and collectors:
- `CollectRawList` — generic list-returning JSON-RPC helper (used for `item.get`, `history.get`, `trend.get`).
- `CollectCount` — wrapper for `countOutput:true` queries.
- `CollectZabbixVersion` — returns `apiinfo.version`.
- `Authenticate` — performs `user.login` and returns token.
- `CollectItemByKey`, `CollectProcessItemsBulk`, `CollectProxyProcessItems` — item lookup helpers used by the report generator.
- `CollectHosts`, `CollectTemplates`, `CollectProxies` — entity collectors used throughout the report.

These helpers are used by the backend to avoid ad-hoc JSON parsing in `cmd/app/main.go` and to improve testability.

Main features
- Data collection and aggregation via the Zabbix API
- Analysis: unsupported items, items without templates, server pollers/processes and proxies, trends, LLD
- Automated recommendations with fix snippets
- Export to HTML/PDF
- Optional persistence of reports (Postgres)

Quick start — run locally
1) Using Docker (easiest):

```bash
docker run -d --name zabbix-easy -p 8080:8080 -e MAX_CCONCURRENT=10 -e ZABBIX_SERVER_HOSTID=10084 -e CHECKTRENDTIME=15d bernardolankheet/zabbix-easy:latest
# open http://localhost:8080
```

2) Running with data persistence (Postgres):

```bash
docker compose --profile db up --build -d
docker run -d --name zabbix-easy -p 8080:8080 -e MAX_CCONCURRENT=10 -e ZABBIX_SERVER_HOSTID=10084 -e CHECKTRENDTIME=15d bernardolankheet/zabbix-easy:latest
# open http://localhost:8080
```

Documentation (MkDocs)
- Online docs: (coming soon)
- To run docs locally: see `docs/en/contribution.md` (English) or `docs/pt_BR/contribution.md` (Portuguese)

Contributing
- Open issues and PRs. See `docs/en/contribution.md` (English) or `docs/pt_BR/contribution.md` (Portuguese) for i18n and development guidelines and how to run the docs locally.

Notes and best practices
- Do not commit your virtualenv (`.venv`) — a `.gitignore` is included.
- Use a Python virtual environment to work with MkDocs.

Contact and license
- Repository: https://github.com/bernardolankheet/zabbix-easy
- License: see `LICENSE`

Changelog
- See `CHANGELOG.md` for recent changes and upgrade notes.