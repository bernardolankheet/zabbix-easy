---
title: "API Call Matrix"
lang: en_US
---

# API Call Matrix (Zabbix methods → collectors → callers)

This matrix maps Zabbix JSON-RPC methods to the collector helpers that encapsulate them and the main callers in the codebase. Use this file as a quick reference to understand what collector to use and where a given API call is exercised.

| Zabbix method | Collector helper | Main caller(s) | Purpose / params |
|---------------|------------------|----------------|------------------|
| `apiinfo.version` | `CollectZabbixVersion(apiUrl, req)` | `app/cmd/app/main.go` (startup/report) | Determine Zabbix version to enable Bearer auth (7.2+) |
| `user.login` | `Authenticate(apiUrl, username, password, req)` | `app/cmd/app/main.go` (Admin check) | Best-effort check for default Admin password; returns token |
| `item.get` | `CollectRawList(apiUrl, token, "item.get", params, req)`<br>`CollectItemByKey(apiUrl, token, key, hostid, req)`<br>`CollectProcessItemsBulk(apiUrl, token, names, hostid, req)`<br>`CollectProxyProcessItems(apiUrl, token, names, hostid, req)` | `app/cmd/app/main.go` (processes, items, probes, SNMP checks, templates resolution) | Fetch items (filters, search wildcards, templated flag, output fields) |
| `history.get` | `CollectRawList(apiUrl, token, "history.get", params, req)` | `app/cmd/app/main.go` (getLastHistoryValue, getHistoryStats, bulk history fallbacks) | Get raw history points (used as fallback when trends absent) |
| `trend.get` | `CollectRawList(apiUrl, token, "trend.get", params, req)` | `app/cmd/app/main.go` (getLastTrend, getTrendsBulkStats) | Get aggregated trend points (min/avg/max) for time window |
| `host.get` | `CollectHosts(apiUrl, token, req)`<br>`CollectHostExists(apiUrl, token, hostid, req)` | `app/cmd/app/main.go` (hosts summary, per-host checks) | Fetch hosts, counts and existence checks |
| `template.get` | `CollectTemplates(apiUrl, token, req)` | `app/cmd/app/main.go` (templates count, template lookups) | Fetch templates and template metadata |
| `proxy.get` | `CollectProxies(apiUrl, token, majorV, req)`<br>`CollectProxiesList(apiUrl, token, req)` | `app/cmd/app/main.go` (proxies summary, per-proxy analysis) | Fetch proxies list (v6/v7 field differences handled in collector) |
| `trigger.get` | `CollectTriggers(apiUrl, token, state, req)`<br>`CollectTriggersCount(apiUrl, token, req)` | `app/cmd/app/main.go` (Triggers tab and KPI calculations) | Collect triggers filtered by state or templates |
| `user.get` | `CollectRawList(apiUrl, token, "user.get", params, req)`<br>`CollectCount(apiUrl, token, "user.get", nil, req)` | `app/cmd/app/main.go` (users count, default admin check) | User listing and counts |

Notes
- Use the collector helpers in `app/internal/collector` instead of calling `zabbixApiRequest` directly. This centralizes auth, transport and parsing.
- For generic list methods we expose `CollectRawList` and for count queries `CollectCount` (which sets `countOutput:true`).
- Process matching (server/proxy) uses wildcard search + client-side resolution inside `CollectProcessItemsBulk` and `CollectProxyProcessItems`.

If you want, I can convert this table into a CSV/MD table embedded in the diagrams or expand each collector entry with example input/output samples.
