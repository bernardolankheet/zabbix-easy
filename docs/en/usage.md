---
title: "Usage"
lang: en_US
---

## Usage

1. Open the web UI
2. Enter the Zabbix API URL and token
3. Wait for the report to be generated
4. Export or print the report as needed

---

## Global environment variables

These variables affect the behavior of the entire report generation:

| Variable | Default | Description |
|----------|---------|-------------|
| `ZABBIX_SERVER_HOSTID` | _(empty)_ | ID of the Zabbix Server host. Used to filter item calls by host. If unset, searches are performed without host filter. |
| `CHECKTRENDTIME` | `30d` | Time window for trend/history analysis. Accepts suffix `d` (days), `h` (hours), `m` (minutes). E.g. `7d`, `24h`. |
| `MAX_CCONCURRENT` | `4` | Max number of concurrent goroutines making Zabbix API calls. Lower to `2`–`3` if the Zabbix server becomes slow or returns timeouts. |
| `API_TIMEOUT_SECONDS` | `60` | HTTP request timeout in seconds for each Zabbix API call. Network timeouts are logged and not retried. Increase to `90`–`120` in large environments. |
| `APP_DEBUG` | _(empty)_ | `1` or `true` to enable detailed logging of each Zabbix API request/response. |

---

## Overall generation flow

The main function is `generateZabbixReport(url, token string, progressCb func(string))` in `cmd/app/main.go`.

- `url` and `token` must be non-empty — the function returns an error immediately if either is empty.
- `url` may be provided as `http://host/` or `http://host/api_jsonrpc.php` — both are accepted; the `/api_jsonrpc.php` suffix is added only when necessary.
- `progressCb` is passed as a function argument (not global), making concurrent runs isolated.

```
POST /api/start
  → validate url and token (returns 400 if empty)
  → create in-memory Task → goroutine: generateZabbixReport(url, token, progressCb)
      → progressCb() updates progress messages (argument, not global)
      → returns HTML fragment
      → saves to PostgreSQL (if DB_HOST configured)

GET /api/progress/:id      → poll status + progress message
GET /api/report/:id        → returns generated HTML fragment (current session)
GET /api/reportdb/:id      → returns saved DB report
GET /api/reportdb/:id?raw=1 → returns bare fragment for inline rendering
```

The generator detects Zabbix version via `apiinfo.version` and adjusts calls and process lists automatically for Zabbix 6 and 7.

---

## Authentication Zabbix API

Starting with Zabbix 7.2 the API authentication uses the token in the HTTP header `Authorization: Bearer <token>` instead of the `auth` field in the JSON-RPC body. The application detects the Zabbix version (`apiinfo.version`) at startup and automatically enables this mode when the version is >= 7.2. Behavior summary:

- `user.login` (username/password test) still uses the login flow and does not require the Bearer header.
- For all other authenticated calls, when Zabbix >= 7.2 the app sends `Authorization: Bearer <token>` and DOES NOT include `auth` in the JSON-RPC body.

### `auth` field authentication (Zabbix < 7.2)

On Zabbix versions prior to 7.2 the API token is passed in the JSON-RPC `auth` field. Typical flow:

- Call `user.login` with `username`/`password` → receive `token` in the response.
- Include that token in subsequent requests in the `auth` field of the JSON-RPC body (e.g. `req["auth"] = "<token>"`).

Example (curl) using `auth` in the body:

```
curl --location --request POST 'http://DNS/api_jsonrpc.php' \
  --header 'Content-Type: application/json' \
  --data '{
  "jsonrpc": "2.0",
  "method": "item.get",
  "params": {"output": "extend", "filter": {"delay": 60}, "templated": false, "countOutput": true},
  "auth": "1a0e00749668dff30b86279804989c7160831d61819d5b44f27d54ec4e07f6d4",
  "id": 1
}'
```

### Bearer authentication (Zabbix >= 7.2)

Example request with Bearer (curl):

```
curl --location --request GET 'http://DNS/api_jsonrpc.php' \
  --header 'Authorization: Bearer 1a0e00749668dff30b86279804989c7160831d61819d5b44f27d54ec4e07f6d4' \
  --header 'Content-Type: application/json' \
  --data '{
  "jsonrpc": "2.0",
  "method": "item.get",
  "params": {"output": "extend", "filter": {"delay": 60}, "templated": false, "countOutput": true},
  "id": 1
}'
```

The application logs the detected version and a `useBearerAuth=true/false` flag for troubleshooting. When `useBearerAuth=true` the `auth` field is intentionally omitted from the JSON body.


## Report Guides

The report is split into 7 tabs. Below is the full documentation for each.

---

## Guide 1: Environment Summary (`tab-resumo`)

### What it is

Consolidated view of the Zabbix environment with key counters. This tab is shown by default when loading a report. Includes two doughnut gauges for quick view of disabled hosts and unsupported items.

### Table displayed

| Parameter | Value | Details |
|-----------|-------|---------|
| Number of hosts | total | enabled / disabled |
| Number of templates | count | — |
| Number of items | total | enabled / disabled / unsupported |
| Number of Proxies | count | — |
| Number of users | count | — |
| Required server performance (NVPS) | float | new values per second |

Gauges:
- Hosts Disabled — doughnut showing enabled vs disabled
- Unsupported Items — doughnut showing supported vs unsupported

### Zabbix API calls

Summary calls are executed in parallel (via `sync.WaitGroup`), reducing initial collection time:

| Goroutine | Calls executed | Data extracted |
|-----------|----------------|----------------|
| 1 | `user.get` | User counts |
| 2 | `host.get` ×3 | Total, enabled and disabled |
| 3 | `template.get` | Template totals |
| 4 | `item.get` ×3 | Total, enabled and disabled items |
| 5 | `item.get` (unsupported) | Total unsupported items |

Calls executed outside summary (always serial because other parts depend on them):

| Call | Relevant params | Data extracted |
|------|-----------------|----------------|
| `apiinfo.version` | _(no auth)_ | Zabbix version; determines `majorV` used across the report |
| `item.get` | `filter:{key_:"zabbix[requiredperformance]"}, hostids:<ZABBIX_SERVER_HOSTID>` | Finds NVPS item |
| `history.get` | `itemids:<id>, sortorder:DESC, limit:1` | Last NVPS value |
| `proxy.get` | `output:extend` | Full proxy list (used by other tabs); count derived from `len(proxies)` |

### Go functions responsible

| Function | Description |
|----------|-------------|
| `getItemByKey(apiUrl, token, "zabbix[requiredperformance]", hostid)` | Find NVPS item with in-memory cache |
| `getLastHistoryValue(apiUrl, token, itemid, histType)` | Fetch last history value for NVPS |
| `getProxies(apiUrl, token)` | Returns full proxy list; proxy count derived from `len(proxies)` |

---

## Guide 2: Zabbix Server (`tab-processos`)

### What it is

Shows utilization levels of internal Zabbix Server processes, split into two groups:

- Pollers (Data Collectors): processes that actively collect metrics (`poller`, `http poller`, `icmp pinger`, `agent poller`, `snmp poller`, etc.)
- Internal Processes: server infrastructure processes (`history syncer`, `housekeeper`, `escalator`, `trapper`, `lld manager`, etc.)

For each process the tab shows `min`, `avg` and `max` utilization (%), plus an OK/Attention visual status (OK if avg < 60%, Attention if avg ≥ 60%).

### Table displayed

| Column | Description |
|--------|-------------|
| Poller / Process | Name with `?` tooltip describing the `zabbix_server.conf` parameter |
| value_min | Minimum utilization in the period (`CHECKTRENDTIME`) |
| value_avg | Average utilization in the period |
| value_max | Peak utilization in the period |
| Status | Green OK / Red Attention / Gray not enabled |

### Zabbix API calls

#### 1. `item.get` — bulk fetch of all processes (1 single call)

Before spawning trend/history goroutines, the code does a single `item.get` that covers all processes (pollers + internal) at once:

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid", "name", "key_", "value_type"],
    "search": {
      "key_": [
        "*agent*poller*",
        "*browser*poller*",
        "*configuration*syncer*",
        "*history*syncer*",
        "*housekeeper*",
        "*poller*",
        "..."
      ]
    },
    "searchByAny": true,
    "searchWildcardsEnabled": true,
    "hostids": "<ZABBIX_SERVER_HOSTID>"
  }
}
```

- Each process name is converted to a wildcard pattern by `nameToWildcard`: `"agent poller"` → `"*agent*poller*"`, matching `agent poller` and `agent_poller` variants.
- `searchByAny: true` makes Zabbix return any item matching at least one pattern.
- Returned items are mapped back to names via `wildcardMatch` (client-side), preferring the most specific pattern to avoid collisions between `*poller*` and `*agent*poller*`.
- If `ZABBIX_SERVER_HOSTID` is unset, the `hostids` parameter is omitted.

#### 2a. `trend.get` bulk — 1 call for all processes (pollers + internal)

After the `item.get` in step 1, all resolved itemids are grouped and sent in a single `trend.get` call:

```json
{
  "method": "trend.get",
  "params": {
    "output": ["itemid", "clock", "value_min", "value_avg", "value_max"],
    "itemids": ["<id1>", "<id2>", "..."],
    "time_from": "<now - CHECKTRENDTIME>",
    "time_to": "<now>"
  }
}
```

The code aggregates points per itemid: `min(value_min)`, `mean(value_avg)`, `max(value_max)`.

#### 2b. `history.get` bulk — fallback for items without trends (max 2 calls)

When an itemid returned no trend records (trends=0 or retention expired), missing ids are grouped by `value_type` and fetched with at most 2 `history.get` calls (one per type: float `0` and integer `3`):

```json
{
  "method": "history.get",
  "params": {
    "output": ["itemid", "clock", "value"],
    "history": 0,
    "itemids": ["<id_missing1>", "<id_missing2>", "..."],
    "time_from": "<now - CHECKTRENDTIME>",
    "time_till": "<now>",
    "sortorder": "ASC",
    "limit": 5000
  }
}
```

Min/avg/max are computed client-side from returned values per itemid.

Result: the entire Zabbix Server section performs at most 3 API calls — 1 `item.get` + 1 `trend.get` + 1 `history.get` — regardless of the number of monitored processes.

### Go functions responsible

| Function | Description |
|----------|-------------|
| `nameToWildcard(name)` | Convert `"agent poller"` → `"*agent*poller*"` for wildcard search |
| `wildcardMatch(pattern, key)` | Simple client-side `*` match used to map returned items back to names |
| `getProcessItemsBulk(apiUrl, token, names, hostid)` | Performs 1 `item.get` with all patterns. Resolves collisions by specificity. Returns `map[nameLowercase]item` |
| `getTrendsBulkStats(apiUrl, token, itemids)` | 1 `trend.get` for all itemids. Aggregates min/avg/max per item. Returns `map[itemid]stats` |
| `getHistoryStatsBulkByType(apiUrl, token, map[itemid]vtype)` | Fallback: groups ids by `value_type` and performs at most 2 `history.get` calls. Returns `map[itemid]stats` |

---

**Compatibility:** tested and working on Zabbix 6.0, 6.4 and 7.0.

- **Internal Process** → add to `procNames`:
```go
procNames := []string{
    ...
    "new process",
}
```

**3. Name rule:** use the name exactly as it appears in the Zabbix item key, with space or underscore. The `nameToWildcard` function converts automatically — `"agent poller"` → `"*agent*poller*"` — matching `agent poller`, `agent_poller` or any variant.

---

## Guide 3: Zabbix Proxies (`tab-proxys`)

### What it is

Shows the status and metrics of Zabbix Proxies configured in the environment. Proxies are grouped into: Unknown, Offline, Active, Passive. For each active/communicating proxy it shows total items, unsupported items and the 10-minute queue.

### Tables displayed

**Summary:**

| Description | Count |
|-------------|-------|
| Proxies Unknown | count |
| Proxies Offline | count |
| Proxies Active | count |
| Proxies Passive | count |
| Total Proxies | count + link |

**Per-proxy detail** (only proxies with `state=2`, communicating):

| Proxy | Type | Total Items | Unsupported Items | Queue-10m |
|-------|------|-------------|-------------------|-----------|
| name | Active / Passive | count | count | value |

### Zabbix API calls

The proxy list is collected at the Summary stage. For each active proxy two parallel calls are made:

| Call | Relevant params | Extracted data |
|------|-----------------|----------------|
| `item.get` | `search:{key_: ["*queue,10m*","*items_unsupported*", ...]}, proxyids:<id>, monitored:true` | `lastvalue` of `zabbix[queue,10m]` and `zabbix[items_unsupported]` |
| `item.get` | `countOutput:true, templated:false, proxyids:<id>` | Total monitored items for the proxy |

### Proxy Processes — keys and item types

Process detection for proxies uses an `item.get` that searches for internal items and dependent items (types `5` and `18`). This ensures the tool finds both dot-style keys (e.g. `process.*.avg.busy`) and function-style keys (`zabbix[process,*,avg,busy]`) by matching both `key_` and `name` fields.

Key points:

- The filter now includes `type: [5, 18]` — Internal (5) and Dependent (18). Previously only `type=5` was queried, which missed dependent items.
- Matching uses wildcards. We use patterns like `*availability*manager*` to cover both notations.
- If the report shows "No process items found", check:
  - The host/proxy has the correct Proxy template (e.g. `Zabbix Proxy Health` or `Remote Zabbix Proxy Health`).
  - The template uses dependent items — in Zabbix dependent items rely on a master item; ensure the master exists and is active.
  - For debugging, call the API directly with `filter: {"type": [5,18]}` and `searchWildcardsEnabled:true`, for example:

```json
{"jsonrpc":"2.0","method":"item.get","params":{
  "output":"extend",
  "hostids":"<HOSTID>",
  "search":{"key_":["*availability*manager*","*poller*","*trapper*"]},
  "searchByAny":true,
  "searchWildcardsEnabled":true,
  "filter":{"type":[5,18]},
  "monitored":true
},"auth":"<TOKEN>","id":1}
```

This returns both dot-style `key_` items and dependent items using `zabbix[...]` in `name`/`key_`.

This change fixes cases where keys like `process.availability_manager.avg.busy` were missed because they were returned as dependent items.

### Version logic

#### Proxy type (Active / Passive)

| Field | Zabbix ≥ 7 | Zabbix 6 |
|-------|-----------|---------|
| Type | `operating_mode` (`0`=Active, `1`=Passive) | `status` (`5`=Active, `6`=Passive) |

#### Connectivity state (Online / Offline / Unknown)

Zabbix 7 returns the `state` field directly in `proxy.get`. Zabbix 6 does **not** return `state`, so the state is derived from `lastaccess` (Unix timestamp of last contact):

| Condition | Inferred state | Zabbix 7 equivalent |
|-----------|----------------|---------------------|
| `state` present and `state == "2"` | **Online** | `state=2` |
| `state` present and `state == "1"` | **Offline** | `state=1` |
| `state` present and `state == "0"` | **Unknown** | `state=0` |
| `state` absent and `lastaccess == 0` | **Unknown** — never connected | — |
| `state` absent and `now - lastaccess > 300s` | **Offline** — lost connection | — |
| `state` absent and `now - lastaccess ≤ 300s` | **Online** | — |

> The 300s (5 min) threshold is conservative: active proxies usually report every few seconds.

### How it works in code

Rows per proxy are generated in parallel goroutines using the semaphore `sem`. Results are reordered by original index to keep display order.

---

### Proxy Processes and Threads

#### What it is

Displays utilization of internal processes of each Zabbix Proxy in an accordion per proxy. For each process it shows `min`, `avg` and `max` utilization (%), plus an **OK** or **Attention** badge in the accordion header.

#### Table displayed (one per proxy)

| Column | Description |
|--------|-----------|
| Process | Name with a `?` tooltip showing the `zabbix_proxy.conf` parameter description |
| value_min | Minimum utilization during `CHECKTRENDTIME` |
| value_avg | Average utilization during the period |
| value_max | Peak utilization during the period |
| Status | Green OK / Red Attention / Gray disabled / Gray no data |

The accordion header shows two badges:

- **Online / Offline·Unknown** — current communication state with Zabbix Server
- **OK / Attention** — worst `value_avg` among all processes with data

#### Zabbix API calls

Each active proxy executes a goroutine (controlled by `sem`) with the flow below:

##### Step 1 — Proxy hostid discovery (3 attempts)

The hostid of the proxy auto-monitoring host can differ from the proxyid starting in Zabbix 7.

| Attempt | Method | Parameters | When it works |
|-----------|--------|-----------|-----------------|


| Description | Count | Link |
|-----------|-----------|------|
| Items without Template | count | filtered link |

**Unsupported items (by type):**

| Item Type | Total | Unsupported | Link |
|----------|-------|-------------|------|
| Zabbix Agent | n | n | link |
| SNMP | n | n | link |
| … | … | … | … |

**Collection Interval / LLD:**

| Interval (s) | Count | Link |
|-------------|-----------|------|
| 1 | n | link |
| 10 | n | link |

**Text Items with History:**

| Template | Item Name | ItemID | Interval (s) | Link |
|----------|----------|--------|-------------|------|

### Zabbix API calls

| Call | Relevant parameters | Data extracted |
|------|---------------------|---------------|
| `item.get` | `filter:{flags:0}, inherited:false, templated:false, countOutput:true` | Items without template |
| `item.get` | `filter:{type:<code>}, countOutput:true, monitored:true` | Total per item type |
| `item.get` | `filter:{type:<code>,state:1}, countOutput:true, monitored:true` | Unsupported per type |
| `item.get` | `filter:{delay:<1\|10\|30\|60>}, countOutput:true` | Items by collection interval |
| `discoveryrule.get` | `filter:{delay:<1\|10\|30\|60\|300>}, countOutput:true, templated:true` | LLD rules by interval |
| `discoveryrule.get` | `filter:{state:1}, countOutput:true, templated:false` | Unsupported LLD rules |
| `item.get` | `templated:true, filter:{value_type:4, delay:[30,60,120,300], history:["1h","1d","7d","31d"]}, selectHosts:["hostid"]` | Text items with history and short delay |
| `template.get` | `filter:{hostid:<ids>}, selectHosts:["hostid"]` | Resolves template names for text items |

### Version logic

- **Browser (type=22):** included in the unsupported table only for Zabbix ≥ 7
- **Frontend links:** `zabbix.php?action=item.list` (Zabbix 7) or `items.php` (Zabbix 6)
- **LLD links:** `host_discovery.php` with version-adapted parameters; delay formatted as `Xs` or `Xm`

### Parallelism

The `item.get` calls per type (total + unsupported) are executed in parallel goroutines controlled by the semaphore `sem`. Rows are re-ordered by `Unsup desc` to place the most problematic types first.

---

## Guide 5: Templates (`tab-templates`)

### What it is

Details of the **Top N templates** with the most unsupported items. For each template it lists the problematic items with a direct edit link to the Zabbix frontend.

### Table displayed (one per template)

| Item | Error | Host | Link |
|------|-------|------|------|
| item name | error message | hostname | [Edit] |

### Zabbix API calls

**No new calls.** All data for this tab is computed from the `item.get` result with `state:1, inherited:true` collected in the initial phase (same data used by the Top Hosts/Templates/Items tab).

The template ranking is built as follows:
1. For each unsupported item, get `tplId = item["templateid"]` — this is the **ID of the item inside the template**, not the template ID itself.
2. Convert to the template ID: `realTplId = cacheTemplateHostID[tplId]`. The `cacheTemplateHostID` cache was populated earlier via `item.get` with `selectHosts`, mapping the item's `templateid` to the template's `hostid` (which is the canonical template ID in Zabbix).
3. Increment `templateCounter[realTplId]` — ensures all items from the same template are grouped correctly, even if they have different `templateid` values.
4. `topTemplates = sort(templateCounter) desc`
5. Template names resolved via `template.get` with `templateids: [...]` (single batch call)

> **Why is `cacheTemplateHostID` needed?** The Zabbix API returns in `item["templateid"]` the ID of the **inherited item inside the template**, not the parent template ID. Multiple items from the same template have different `templateid` values — without the conversion, the same template would appear multiple times in the ranking. The correct mapping is: `cacheTemplateHostID[item_templateid] → template_hostid`.

### Building edit links

| Version | Link format |
|---------|-------------|
| Zabbix ≥ 7 | `zabbix.php?action=item.list&context=host&filter_hostids[]=<hostid>&filter_name=<item>` |
| Zabbix 6 | `items.php?form=update&hostid=<hostid>&itemid=<itemid>&context=host` |

---

## Guide 6: Top Hosts/Templates/Items (`tab-top`)

### What it is

Displays four rankings based on the unsupported items collected:

1. **Top Offending Templates** — templates with the most problematic items
2. **Top Offending Hosts** — hosts with the most problematic items (shows the most recurring template per host)
3. **Top Problematic Items** — item keys with the highest error count
4. **Most Common Error Types** — most frequent error messages

### Tables displayed

**Top Offending Templates:**

| Template | Error Count |
|----------|-------------|

**Top Offending Hosts:**

| Host | Top Offending Template | Error Count |
|------|------------------------|-------------|

**Top Problematic Items:**

| Item | Template | Error Count |
|------|----------|-------------|

**Most Common Error Types:**

| Error Message | Template | Occurrences |
|---------------|----------|-------------|

### Zabbix API calls

**No new calls.** All data comes from groupings performed during the initial phase on the `item.get` with `state:1`. The counters are:

- `templateCounter[realTplId]` — errors per template (using the canonical template ID via `cacheTemplateHostID`)
- `hostCounter[hostname]` — errors per host
- `itemCounter[itemname|realTplId]` — errors per item within the template
- `errorCounter[errormsg|realTplId]` — errors per message within the template

The `realTplId` is derived from `cacheTemplateHostID[item["templateid"]]`, ensuring all items from the same template are grouped under a single key. Without this conversion, the same template could appear multiple times in the rankings with separate counts.

The Top N is 10 by default (constant `topN = 10`).

---

## Guide: Users (`tab-usuarios`)

### What it is

This tab checks whether the Zabbix default administrative account (`Admin`) exists and performs a best-effort test to detect if it still accepts the default password `zabbix`.

Key points:
- The report does **not** list all users — it performs a targeted `user.get` filtered by `username = Admin` to avoid scanning the entire user table.
- If the `Admin` account appears enabled, the report attempts a `user.login` with `Admin`/`zabbix`. The returned token is discarded; the check is only to detect the default-password exposure.
- If the `Admin` account is missing or clearly disabled, the password test is skipped and the KPI is considered safe.

### How "enabled" is detected (best-effort)

The code inspects common fields returned by `user.get` (`status`, `disabled`) and accepts several representations (numeric, boolean or string). Concretely, values indicating disabled (examples) are interpreted as:

- `status != 0` (numeric)
- `disabled == true` (boolean) or `disabled == "1"`/`"true"` (string)

If none of those markers indicate the account is disabled and the username equals `Admin`, the account is considered present and enabled for the default-password test.

### Table displayed

| Column | Description |
|--------|-------------|
| User | Username (e.g. `Admin`) |
| Full Name | User's full name (name + surname) |
| Default Password | Badge: shows whether `Admin` accepts the default password `zabbix` (Yes / No) |

### Zabbix API calls used

| Call | Params | Purpose |
|------|--------|---------|
| `user.get` | `countOutput:true` (summary) | Counts users shown in the summary table |
| `user.get` | `filter:{username:"Admin"}, output:["userid","username","name","surname"]` | Fetches only the `Admin` account to display the single-row table (avoids fetching full user list) |
| `user.login` | `username:"Admin", password:"zabbix"` | Best-effort authentication attempt to determine whether the default password is still valid (token discarded) |

Notes:
- The password check is non-destructive and only tests authentication; it may fail silently depending on API rate limits, network errors, or insufficient permissions. Failures are logged at debug level.

### UI behavior and recommendations

- If `Admin` exists and accepts `zabbix`, the tab displays a critical alert (red) and the table marks the default-password column as `Yes`.
- If `Admin` exists but the default password is rejected, a milder warning box is shown and the table shows `No`.
- If `Admin` is absent or disabled, the tab shows a localized `users.no_data` message.

The report also surfaces a recommendation entry when the default `Admin` account with default password is detected, advising to change or disable the account.

---

## Guide 7: Recommendations (`tab-recomendacoes`)

### What it is

Automatic suggestions generated based on all collected data. All sections and sub-items are **displayed only when there is a recommendation** — if the value is 0, neither the section nor its sub-item appears. KPI cards at the top provide a quick overview.

1. **Zabbix Server** — processes in Warning + async poller suggestion (Zabbix 7) — only shown if there are processes with avg ≥ 60% or async pollers disabled
2. **Zabbix Proxies** — processes in Warning + async pollers disabled (Zabbix 7) + Unknown/Offline proxies + proxies without template — only shown when there is an issue
3. **Items** — each sub-item (without template, unsupported, disabled, short interval, text with history, SNMP) only appears individually if its counter is > 0; the whole section disappears if all are 0
4. **LLD Rules** — each sub-item (short interval, unsupported) only appears individually if > 0; section disappears if both are 0
5. **Templates** — only shown if there are templates to review, identified errors, or SNMP templates to migrate (Zabbix 7)

### KPI cards

KPIs are displayed in a horizontal strip at the top of the Recommendations tab. Each card is **clickable** and scrolls the page to the corresponding section. The display order is always:

| # | Label | Color | Go variable | Click leads to | Display condition |
|---|-------|-------|-------------|----------------|-------------------|
| 1 | Zabbix Server - Process/Pollers with high AVG | 🟡 Yellow / 🟢 Green (`kpi-warn` / `kpi-ok`) | `attentionCount` = `len(attention)` | Zabbix Server section | Always (green if 0) |
| 2 | Offline Proxies | 🔴 Red (`kpi-crit`) | `proxyOfflineCount` | Zabbix Proxies section | Always |
| 3 | Unknown Proxies | ⚪ Neutral | `proxyUnknownCount` | Zabbix Proxies section | Always |
| 4 | Proxies - Process/Pollers with high AVG | 🟢 Green / 🟡 Yellow | `len(proxyProcAttnList)` | Zabbix Proxies section | Always |
| 5 | Unsupported Items | 🔴 Red (`kpi-crit`) | `unsupportedCount` | Items section | Always |
| 6 | SNMP Templates for Async Poller | 🟢 Green / 🟡 Yellow | `len(snmpMigrationTpls)` | Templates section | **Zabbix ≥ 7 only** |
| 7 | Items - SNMP-POLLER | 🟢 Green / 🔴 Red | `snmpPct` (%) | Items section | **Zabbix ≥ 7 only** |
| 8 | Text Items with History | 🟡 Yellow (`kpi-warn`) | `textItemsCount` | Items section | Always |

### KPI color logic

| KPI | Condition | CSS class | Border |
|-----|-----------|-----------|--------|
| Zabbix Server - Process/Pollers | `attentionCount == 0` → green; `> 0` → yellow | `kpi-ok` / `kpi-warn` | green / yellow |
| Offline Proxies | always red | `kpi-crit` | red `#ff6666` |
| Unknown Proxies | always neutral | _(no class)_ | gray |
| Proxies - Process/Pollers | `len(proxyProcAttnList) == 0` → green; `> 0` → yellow | `kpi-ok` / `kpi-warn` | green / yellow |
| Unsupported Items | always red | `kpi-crit` | red `#ff6666` |
| SNMP Templates for Async Poller | `len(snmpMigrationTpls) == 0` → green; `> 0` → yellow | `kpi-ok` / `kpi-warn` | green / yellow |
| Items - SNMP-POLLER (%) | `snmpPct >= 80%` → green; `< 80%` → red | `kpi-ok` / `kpi-crit` | green / red |
| Text Items with History | always yellow | `kpi-warn` | yellow `#ffcc00` |

> **Threshold reference:** **Zabbix Server** and **Proxy** processes in Warning use avg ≥ 60% (variables `attention` and `proxyProcAttnList`).

### Zabbix API calls (exclusive to this tab)

Only for Zabbix ≥ 7, two `item.get` calls for the SNMP KPIs (shared with the "3.x) Items SNMP-POLLER" entry in the Items Section and the "Templates suitable for migration to SNMP-POLLER" sub-section):

| Call | Relevant parameters | Go variable filled |
|------|---------------------|--------------------|
| `item.get` | `filter:{type:20}, templated:true, countOutput:true` | `snmpTplCount` |
| `item.get` | `filter:{type:20}, search:{snmp_oid:["get[*","walk[*"]}, searchWildcardsEnabled:true, searchByAny:true, countOutput:true, templated:true` | `snmpGetWalkCount` |
| `item.get` ×2 + `template.get` | _(see "Templates suitable for migration" sub-section)_ | `snmpMigrationTpls` |

### Helper functions used

| Function | Description |
|--------|-----------|
| `pct(part, total int) string` | Formats percentage `"0.00%"`; returns `"0%"` if total=0 |
| `titleWithInfo(tag, title, tip string) string` | Generates HTML heading with `?` icon and tooltip |

### How to add a new KPI

1. **Calculate the data** before the `html += "<div class='rec-kpis'>"` block in `cmd/app/main.go`.
2. **Define the CSS class** (`kpi-ok`, `kpi-warn`, `kpi-crit` or empty for neutral) based on the value.
3. **Insert the `<div>`** at the desired position inside the `rec-kpis` block, following the pattern:
```go
html += `<div class='kpi ` + classe + `' data-target='#card-<section>' title='<tooltip>'>`+
    `<div class='kpi-num'>` + value + `</div>`+
    `<div class='kpi-label'>KPI Label</div></div>`
```
4. Click-to-scroll to the section is managed automatically by the inline JavaScript right after the `rec-kpis` block — **no JS changes are needed**.

---

### Section 1 — Zabbix Server

#### Display condition

The section **does not appear** when there is nothing to recommend. The entire block is omitted if:

```
len(attention) == 0  AND  len(missingAsync) == 0
```

The section is only generated when at least one of these conditions is true:
- There are processes/pollers with `value_avg ≥ 60%` (list `attention`)
- There are async pollers (`Agent Poller`, `HTTP Agent Poller`, `SNMP Poller`) disabled on Zabbix 7+ (list `missingAsync`)

#### Generated HTML hierarchy

```
1) Zabbix Server
  1.1) zabbix_server.conf suggestions:      ← only shown if len(attention) > 0
       Customize Processes and Threads: ⓘ  ← bold text with tooltip, no own numbering
         1. <Process name> — avg: X%
         2. <Process name> — avg: X%
  1.2) Use Async Pollers: ⓘ                ← only shown if len(missingAsync) > 0
       • Agent Poller
       • SNMP Poller
```

> When both exist, the sub-numbers are `1.1)` and `1.2)`. When only one exists, only `1.1)` appears.

#### Sub-item "zabbix_server.conf suggestions"

- Numbered as `1.1)` via `nextSub(&serverSub, "zabbix_server.conf suggestions:")`
- Shown only when `len(attention) > 0`
- Directly below the numbered header appears the text **"Customize Processes and Threads:"** as `<strong>` with a `?` icon (tooltip with guidance on when to increase the process)
- Followed by `<ol>` with one item per process in Warning: `Process name — avg: X%`
- Processes ordered by `value_avg` descending (highest utilization first)

#### Sub-item "Use Async Pollers"

- Numbered as `1.1)` or `1.2)` depending on whether "Suggestions" also appears
- Shown only when `len(missingAsync) > 0`
- Only relevant for **Zabbix ≥ 7** (async pollers do not exist in Zabbix 6)
- Lists the disabled pollers: `Agent Poller`, `HTTP Agent Poller`, `SNMP Poller`
- Each item has a `?` icon with a usage description

#### Go variables involved

| Variable | Type | Origin |
|----------|------|--------|
| `attention` | `[]struct{Name string; Vavg float64}` | Built by iterating `pollRows` + `procRows` where `StatusText == "Warning"` |
| `missingAsync` | `[]string` | Built by checking whether `Agent Poller`, `HTTP Agent Poller`, `SNMP Poller` are in `pollRows` with `Disabled == true` |
| `pollMap` | `map[string]pollRow` | Auxiliary map `friendly_name_lowercase → pollRow` for `missingAsync` lookup |

---

### Section 2 — Zabbix Proxies

#### Display condition

The section **does not appear** when there is nothing to recommend. The entire block is omitted if:

```
unknown == 0  AND  offline == 0  AND
len(proxyProcAttnList) == 0  AND  len(proxyNoTemplateList) == 0  AND
len(proxyMissingAsyncMap) == 0
```

#### Generated HTML hierarchy

```
2) Zabbix Proxies
  2.1) Customize Processes and Threads ⓘ   ← only shown if len(proxyProcAttnList) > 0
       1. PROXY-NAME — Friendly Name — avg: X%
       2. ...
  2.2) Use Async Pollers: ⓘ                ← only shown if len(proxyMissingAsyncMap) > 0
       • PROXY-NAME — Agent Poller — Snmp Poller
       • ...
  2.3) Unknown Proxy Status ⓘ              ← only shown if unknown > 0
       N proxies with Unknown status detected
       • proxy-name
  2.4) Offline Proxies ⓘ                  ← only shown if offline > 0
       N proxies with Offline status detected
       • proxy-name
  2.5) Zabbix Proxy without Monitoring ⓘ  ← only shown if len(proxyNoTemplateList) > 0
       The proxies below do not have the Zabbix Proxy Health template linked...
       1. proxy-name (link to the host in Zabbix)
```

> Sub-numbers are generated automatically by `nextSub(&proxySub, ...)` — if only some sub-items appear, numbering is sequential starting from `2.1)`.

In addition to these sub-items, the report also displays a highlighted recommendations area (yellow blocks) for quick proxy-related actions. This area uses CSS classes `.rec-highlight-list` and `.rec-highlight-item` (in `app/web/static/style.css`) and contains, for example:

- One block per proxy with `Start...=   # increase until avg drops below 60%` lines grouped and deduplicated (one line per `Start*` parameter), followed by `systemctl restart zabbix-proxy`.
- A compact block with common actions and diagnostics: suggestion to enable async pollers (Zabbix ≥ 7) and verification commands (`systemctl status zabbix-proxy`, `tail -100 /var/log/zabbix/zabbix_proxy.log`, `nc -zv <server> 10051`).
- Titles and the suggestion comment are localized via the i18n keys `fix.proxy_highlight_*` and `fix.proxy_increase_hint`.

#### Sub-item "Customize Processes and Threads"

- Shown only when `len(proxyProcAttnList) > 0`
- Same behavior as the equivalent sub-item in Section 1 (Zabbix Server), but per proxy
- Ordered `<ol>` with: `PROXY-NAME — Process Friendly Name — avg: X%`
- Built with avg ≥ 60% per process, per proxy

#### Sub-item "Use Async Pollers"

- Shown only when `len(proxyMissingAsyncMap) > 0`
- Only relevant for **Zabbix ≥ 7** and **only for online proxies** with collected items
- Compact `<ul>` with one `<li>` per proxy: `**PROXY-NAME** — Agent Poller ⓘ — Snmp Poller ⓘ`
- Each poller has a tooltip with a usage description
- A proxy appears in the list when at least one of the three async pollers (`Agent Poller`, `Http Agent Poller`, `Snmp Poller`) **was not found** in the collected data or has `vavg < 0` (process not enabled)

#### Sub-item "Unknown Proxy Status"

- Shown only when `unknown > 0`
- Text: _"N proxies with Unknown status detected"_
- Followed by `<ul>` with proxy names (`unknownNames`)

#### Sub-item "Offline Proxies"

- Shown only when `offline > 0`
- Text: _"N proxies with Offline status detected"_
- Followed by `<ul>` with proxy names (`offlineNames`)

#### Sub-item "Zabbix Proxy without Monitoring"

- Shown only when `len(proxyNoTemplateList) > 0`
- Reference template: **Zabbix Proxy Health** (Zabbix 7) / **Template App Zabbix Proxy** (Zabbix 6)
- The template link uses the environment URL (`ambienteUrl`) with automatic filtering by name
- Each proxy in the list is displayed as a clickable link to the Zabbix host screen filtered by the proxy name
- A proxy enters this list if `res.noItemsNote != ""` — i.e. the collection loop found no process items for that proxy host

#### Go variables involved

| Variable | Type | Origin |
|----------|------|--------|
| `proxyProcAttnList` | `[]proxyProcAttnItem{ProxyName, ProcFriendly, Vavg}` | Built by iterating `ppResults` — rows with `vavg >= 59.9` |
| `proxyMissingAsyncMap` | `map[string][]string` | `proxyName → []pollerNames` — pollers missing or disabled in the proxy (Zabbix 7+, online, with items) |
| `asyncProcNames` | `[]string` | `{"agent poller", "http agent poller", "snmp poller"}` — async pollers checked |
| `unknown` | `int` | Count of proxies with `availability == 0` (Unknown) |
| `unknownNames` | `[]string` | Names of Unknown proxies |
| `offline` | `int` | Count of proxies with `availability == 2` (Offline) |
| `offlineNames` | `[]string` | Names of Offline proxies |
| `proxyNoTemplateList` | `[]proxyNoTemplateItem{ProxyName, HostId}` | Online proxies whose `res.noItemsNote != ""` — no process items collected |

#### Logic of `proxyMissingAsyncMap`

For each proxy in `proxyMetaList` that is **online** (`pm.Online == true`), with **Zabbix ≥ 7** and with **collected items** (`res.noItemsNote == ""` and `len(res.rows) > 0`):

```go
for _, apn := range asyncProcNames {   // "agent poller", "http agent poller", "snmp poller"
    found := false
    hasData := false
    for _, row := range res.rows {
        if strings.ToLower(row.friendly) == apn {
            found = true
            if row.vavg >= 0 { hasData = true }
            break
        }
    }
    if !found || !hasData {
        // capitalize and add to this proxy's "missing" list
    }
}
if len(missing) > 0 {
    proxyMissingAsyncMap[pm.Name] = missing
}
```

A poller is considered missing when:
- It was not found in `res.rows` (`!found`), **or**
- It was found but `vavg < 0` — the item returned "Create item in template or Process not enabled" (`!hasData`)

---

### Section 3 — Items

#### Section display condition

The entire section is only generated when at least one sub-item has data:

```go
itemsHasData := itemsNoTplCount > 0 || unsupportedVal > 0 || disabledCount > 0 ||
                itemsLe60 > 0 || textCount > 0 || (majorV >= 7 && snmpTplCount > 0)
```

#### Sub-items and their individual conditions

| Sub-item | Condition to appear | Go variable |
|---------|------------------------|-------------|
| Items without Template | `itemsNoTplCount > 0` | `itemsNoTplCount` |
| Unsupported items | `unsupportedVal > 0` | `unsupportedVal` |
| Disabled items | `disabledCount > 0` | `disabledCount` |
| Items with Interval ≤ 60s | `itemsLe60 > 0` | `itemsLe60` |
| Text Items with History (≤ 300s) | `textCount > 0` | `textCount` |
| Items SNMP-POLLER (Zabbix 7) | `majorV >= 7 && snmpTplCount > 0` | `snmpTplCount` |

---

### Section 4 — LLD Rules

#### Section display condition

The entire section is only generated when at least one sub-item has data:

```go
lldLe300 > 0 || lldNotSupCnt > 0
```

#### Sub-items and their individual conditions

| Sub-item | Condition to appear | Go variable |
|---------|------------------------|-------------|
| LLD Rules with Interval ≤ 300s | `lldLe300 > 0` | `lldLe300` |
| Unsupported LLD Rules | `lldNotSupCnt > 0` | `lldNotSupCnt` |

---

### Section 5 — Templates

#### Section display condition

The entire section is only generated when at least one sub-item has data:

```go
tplHasData := len(topTemplates) > 0 || len(topErrors) > 0 ||
              (majorV >= 7 && len(snmpMigrationTpls) > 0)
```

#### Sub-items and their individual conditions

| Sub-item | Condition to appear | Go variable |
|---------|------------------------|-------------|
| Templates for review | `len(topTemplates) > 0` | `topTemplates` |
| Most Common Errors | `len(topErrors) > 0` | `topErrors` |
| Templates suitable for SNMP-POLLER migration | `majorV >= 7 && len(snmpMigrationTpls) > 0` | `snmpMigrationTpls` |

---

### Item "Items SNMP-POLLER (Zabbix 7)" — Section 3 (Items)

#### What it is

Appears in **Section 3 — Items** of the Recommendations (sub-item `3.x)`), **only** for Zabbix ≥ 7 and when there is at least 1 SNMP item in templates (`snmpTplCount > 0`). Displays:

- Total SNMP items in templates (`snmpTplCount`)
- Count already using `get[]`/`walk[]` OID — async poller format (`snmpGetWalkCount`)
- Percentage migrated relative to the total items in the environment

Points the operator to the **"Templates suitable for migration to SNMP-POLLER"** sub-section to see the specific templates that need attention.

#### Display condition

```
majorV >= 7  AND  snmpTplCount > 0
```

#### API calls

These 2 calls are **shared** with the "Items - SNMP-POLLER" KPI card at the top of Recommendations — values are collected once and reused:

| Call | Relevant parameters | Go variable |
|------|---------------------|-------------|
| `item.get` | `filter:{type:20}, templated:true, countOutput:true` | `snmpTplCount` |
| `item.get` | `filter:{type:20}, search:{snmp_oid:["get[*","walk[*"]}, searchWildcardsEnabled:true, searchByAny:true, countOutput:true, templated:true` | `snmpGetWalkCount` |

> **`type:20`** corresponds to the SNMP type in Zabbix. The OIDs `get[*` and `walk[*` are the Zabbix 7 standard for the async poller; OIDs in the old format (e.g. `.1.3.6.1.2.1.1.1.0`) do **not** match this filter.

#### HTML display logic

```go
if majorV >= 7 && snmpTplCount > 0 {
    // displays item 3.x with snmpTplCount, snmpGetWalkCount and pct(snmpGetWalkCount, totalItemsVal)
    // points to the "Templates suitable for migration to SNMP-POLLER" section
}
```

---

### Sub-section "Templates suitable for migration to SNMP-POLLER" — Section 5 (Templates)

#### What it is

Appears at the end of **Section 5 — Templates** of the Recommendations, **only** for Zabbix ≥ 7 and when there is at least 1 legacy template in use (`len(snmpMigrationTpls) > 0`). Lists alphabetically the names of templates that satisfy **all** of these conditions:

1. Have at least one SNMP item (`type=20`) in templates
2. **Have not yet** migrated OIDs to `get[]` or `walk[]` format (Zabbix 7 async poller)
3. Are **linked to at least one host** (actually in use in the environment)

Fully migrated templates (where **all** SNMP items already use `get[]`/`walk[]`) are automatically excluded.

> **Note:** A template with a **mix** of old and new OIDs still appears in the list. Only when **100% of SNMP items** use the modern format is the template removed. This ensures incomplete migrations are also reviewed.

#### Zabbix API calls (exclusive to this sub-section)

**3 calls** made sequentially after the `countOutput` calls for the KPI, **only** when `majorV >= 7`:

##### Call 1 — All SNMP items in templates (with template hostid)

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid"],
    "filter": { "type": 20 },
    "templated": true,
    "selectHosts": ["hostid"]
  }
}
```

- `selectHosts: ["hostid"]` returns, for each item, the `hosts` field with the parent template's `hostid`.
- Used to build the set of **all hostids of SNMP templates** (`allSnmpHostids`).

##### Call 2 — SNMP items already using get[]/walk[] (template hostid)

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid"],
    "filter": { "type": 20 },
    "search": { "snmp_oid": ["get[*", "walk[*"] },
    "searchWildcardsEnabled": true,
    "searchByAny": true,
    "templated": true,
    "selectHosts": ["hostid"]
  }
}
```

- `search` + `searchWildcardsEnabled` filters only OIDs that **start with** `get[` or `walk[`.
- Result → set `modernHostids` (hostids of templates that have at least 1 modern item).
- The difference `allSnmpHostids − modernHostids` = `legacyHostSet` (templates with only legacy OIDs).

##### Call 3 — Resolving legacy template names

```json
{
  "method": "template.get",
  "params": {
    "output": ["templateid", "name"],
    "templateids": ["<ids from legacyHostSet>"],
    "selectHosts": ["hostid"]
  }
}
```

- `selectHosts: ["hostid"]` returns the `hosts` field per template.
- Templates with `hosts == []` (not linked to any host) are **discarded** — they don't appear in the list.
- Remaining template names are sorted alphabetically and stored in `snmpMigrationTpls`.

#### Complete build flow

```
Call 1 → allSnmpHostids   (all hostids of templates with SNMP items)
Call 2 → modernHostids    (hostids of templates with at least 1 get[]/walk[] OID)

legacyHostSet = allSnmpHostids − modernHostids

Call 3 → template.get(legacyHostSet)
              → discard len(hosts) == 0   (not linked to any host)
              → sort.Strings()
              → snmpMigrationTpls
```

#### Resulting Go variable

| Variable | Type | Content |
|----------|------|---------|
| `snmpMigrationTpls` | `[]string` | Alphabetically sorted list of legacy template names **in use** |

#### HTML display condition

```go
if majorV >= 7 && len(snmpMigrationTpls) > 0 {
    // renders h5 with titleWithInfo + <ul> with template names
}
```

If `snmpMigrationTpls` is empty (all templates already migrated or no Zabbix 7 environment), the sub-section **does not appear** in the report.

#### How to interpret the list

| Situation | Meaning |
|-----------|---------|
| Sub-section not displayed | All SNMP templates in use already use `get[]`/`walk[]` — well done! |
| Template appears in list | Has at least 1 SNMP item with a legacy OID **and** is linked to at least 1 host |
| Green KPI (≥ 80%) + list present | Most migrated, but specific templates still have legacy OID remnants |
| Red KPI (< 80%) + long list | Environment with most SNMP templates still using the synchronous poller |

#### How to add/remove detection logic

The complete block is in `cmd/app/main.go`, right after the two `item.get countOutput` calls for the SNMP KPI, inside the `if majorV >= 7 { ... }` block. To change the "legacy" criterion (e.g. include another type of old OID), adjust the `search.snmp_oid` of **Call 2**:

```go
// Call 2 — add a new modern OID pattern:
"search": map[string]interface{}{
    "snmp_oid": []string{"get[*", "walk[*", "new_format[*"},
},
```

---

## Saved Reports (Database Persistence)

### Overview

Every time a report is successfully generated, it is **automatically saved** to PostgreSQL — no extra action is required from the user. This allows accessing previous reports at any time, even after restarting the application.

> **The database is optional.** The app works normally without PostgreSQL — reports are only available in the current session (in-memory) and are lost when the container restarts.
>
> To enable persistence, set `DB_HOST` (and optionally the other `DB_*` variables) and bring up the `postgres` service with the `db` profile (see details in [Installation](installation.md)).

When `DB_HOST` is **not** configured:
- The **Saved Reports** card is **automatically hidden** in the web interface
- The endpoints `/api/reports`, `/api/reportdb/:id` and `DELETE /api/reports` return `db_enabled: false` or `404`
- No error is shown to the user — the app simply operates without a database

---

### How to use the Saved Reports interface

On the home screen, below the **Generate Report** button, there is a **Saved Reports** card with the following controls:

```
┌─ SAVED REPORTS ──────────────────────── [↺ Load list] ─┐
│                                                          │
│  [── select a report ──────────────]  [Open] [Delete]   │
│                                        [Delete All]      │
└──────────────────────────────────────────────────────────┘
```

#### Step by step

| Step | Action | What happens |
|------|--------|--------------|
| 1 | Click **↺ Load list** | The list of the last 20 saved reports is loaded into the selector. Each entry shows the Zabbix URL and the generation date/time. |
| 2 | Select a report from the dropdown | The report is highlighted — no action yet. |
| 3 | Click **Open** | The selected report is loaded inline, with all tabs, Export HTML and Print PDF buttons working normally. |
| 4 _(optional)_ | Click **Delete** | Removes **only the selected report** from the database after confirmation. The list is automatically updated. |
| 5 _(optional)_ | Click **Delete All** | Removes **all reports** from the database after double confirmation. Returns the count of removed records. |

> **Tip:** After opening a saved report, all **Export HTML** and **Generate PDF** buttons work normally — the report reopens with the same layout and functionality as the original generation.

---

### Report action bar

Whenever a report is displayed (generated by the API or loaded from the database), an action bar appears in the report header with three buttons:

```
┌─ Zabbix Report ── Generated on: dd/mm/yyyy hh:mm:ss ──────────────────────────────────┐
│                                                                [NEW] [HTML] [PDF]      │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

| Button | Icon | Action | Available in exported HTML? |
|--------|------|--------|-----------------------------|
| **New Report** | Document with `+` | Closes the current report, clears the screen and returns to the generation form | ❌ No |
| **Export HTML** | HTML file icon | Generates a static, self-contained `.html` file (embedded CSS, working charts) and triggers automatic download | ❌ No |
| **Generate PDF** | Printer icon | Opens the browser print dialog — recommended to select "Save as PDF" | ❌ No |

> The **New Report** button **does not exist** in exported HTML files. It is created dynamically only when the report is displayed inline in the web interface — either via API generation or via database loading.

---

### When reports are saved

Saving happens automatically at the end of the `generateZabbixReportWithProgress` function, in the following flow:

```
POST /api/start
  → goroutine: generateZabbixReport()
      → report HTML generated successfully
      → INSERT INTO reports (name, format, content, zabbix_url) VALUES (...)
         name     = "report-<unix timestamp>"
         format   = "html"
         content  = full HTML (BYTEA)
         zabbix_url = URL entered in the form
```

If the database is not available, the report is still displayed on screen normally — the persistence error is only logged (`[WARN] Failed to save report to DB`).

---

### Environment variables for the database

| Variable    | Default     | Description |
|-------------|-------------|-------------|
| `DB_HOST`   | _(empty)_   | PostgreSQL host. **If not set, persistence is disabled.** |
| `DB_PORT`   | `5432`      | PostgreSQL port. |
| `DB_USER`   | `postgres`  | Database user. |
| `DB_PASSWORD` | `postgres` | Database password. |
| `DB_NAME`   | `zabbix_report` | Database name. |

Example in `docker-compose.yml` (variables are **commented out by default**; uncomment to enable):
```yaml
environment:
  # Uncomment to enable report persistence in PostgreSQL:
  #- DB_HOST=postgres
  #- DB_PORT=5432
  #- DB_USER=postgres
  #- DB_PASSWORD=postgres
  #- DB_NAME=zabbix_report
```

> **How to start with the database:** `docker compose --profile db up --build -d`
> The `db` profile is required because the `postgres` service is only started when explicitly requested.
> The `go-app` does **not** depend on postgres in `depends_on` — if the database is not available, the app starts normally without persistence.

---

### Database schema

The table is created automatically on first run if it does not exist:

```sql
CREATE TABLE reports (
  id         SERIAL PRIMARY KEY,
  name       TEXT,            -- "report-<timestamp>" auto-generated
  format     TEXT,            -- always "html" for now
  content    BYTEA,           -- full report HTML
  zabbix_url TEXT,            -- Zabbix URL used during generation
  created_at TIMESTAMPTZ DEFAULT now()
);
```

> **Automatic migration:** If the table exists with missing columns (old schema), the application renames the old table to `reports_old_<timestamp>` and creates a new one with the correct schema.

---

### REST API — Report endpoints

For external integrations or automation, the available endpoints are:

| Endpoint | Method | Database required | Description |
|----------|--------|-------------------|-------------|
| `POST /api/start` | POST | No | Starts generation; body `{"zabbix_url":"...","zabbix_token":"..."}` → returns `{"task_id":"..."}` |
| `GET /api/progress/:id` | GET | No | Polls task status → `{"status":"done\|running\|error","progress_msg":"..."}` |
| `GET /api/report/:id` | GET | No | Returns the HTML of the report in the current session (in-memory, lost on restart) |
| `GET /api/db-status` | GET | No | Informs the frontend whether the database is active → `{"db_enabled": true\|false}` |
| `GET /api/reports` | GET | Optional | Without DB: `{"db_enabled":false,"reports":[]}`. With DB: `{"db_enabled":true,"reports":[...]}` |
| `GET /api/reportdb/:id` | GET | **Yes** | Returns the saved report as a complete HTML document (to open directly in the browser) |
| `GET /api/reportdb/:id?raw=1` | GET | **Yes** | Returns only the internal HTML fragment (used by JS to render inline with the standard layout) |
| `DELETE /api/reportdb/:id` | DELETE | **Yes** | Removes a specific report by ID → `{"deleted":"<id>"}` or `404` if not found |
| `DELETE /api/reports` | DELETE | **Yes** | Removes **all** reports → `{"deleted":<count>}` |

#### Example: list and open a report via curl

```bash
# 1. List saved reports
curl http://localhost:8080/api/reports

# 2. Open report with ID 3 in the browser
curl http://localhost:8080/api/reportdb/3 > report.html

# 3. Delete the report with ID 3
curl -X DELETE http://localhost:8080/api/reportdb/3

# 4. Delete all reports
curl -X DELETE http://localhost:8080/api/reports
```

The report is divided into tabs. This section documents each one: what it displays, which Zabbix API calls are made, which Go function generates the HTML, and how the data is processed.

---


### What it is

Displays the utilization level of internal Zabbix Server processes, divided into two groups:

- **Pollers (Data Collectors):** processes responsible for actively collecting metrics from agents (poller, http poller, icmp pinger, agent poller, snmp poller, etc.)
- **Internal Processes:** server infrastructure processes (history syncer, housekeeper, escalator, trapper, lld manager, etc.)

For each process, the **minimum**, **average** and **maximum** utilization (%) is shown, along with a visual status of **OK** (< 50%) or **Warning** (≥ 50%).

### Table displayed

| Column | Description |
|--------|------------|
| Poller / Process | Process name with a `?` tooltip icon |
| value_min | Minimum utilization in the analyzed period |
| value_avg | Average utilization in the analyzed period |
| value_max | Peak utilization in the analyzed period |
| Status | `OK` (green, avg < 50%) or `Warning` (red, avg ≥ 50%) or gray when not enabled |

### Environment variables that affect this tab

| Variable | Default | Description |
|----------|---------|-------------|
| `ZABBIX_SERVER_HOSTID` | _(empty)_ | Host ID of the Zabbix Server in Zabbix. If not set, the search ignores the host filter and may return any host that has the key. Recommended to set for accuracy. |
| `CHECKTRENDTIME` | `30d` | Time window for trend/history analysis. Accepts suffix `d` (days), `h` (hours), `m` (minutes). E.g. `7d`, `24h`. |
| `MAX_CCONCURRENT` | `6` | Number of processes that can run concurrently in parallel Zabbix API calls. |

### Zabbix API calls

Each process in the list queries the **pre-loaded map** by `getProcessItemsBulk` (1 call made before the goroutines) and then makes **two parallel calls** to get trend/history data:

#### 1. `item.get` bulk — locate all process items (1 single call)

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid", "name", "key_", "value_type"],
    "search": { "key_": ["*agent*poller*", "*poller*", "*history*syncer*", "..."] },
    "searchByAny": true,
    "searchWildcardsEnabled": true,
    "hostids": "<ZABBIX_SERVER_HOSTID>"
  }
}
```

- The key is **no longer** `zabbix[process,<name>,avg,busy]` — it is searched by wildcard and matches any key format (space or underscore).
- If `ZABBIX_SERVER_HOSTID` is not set, the `hostids` parameter is omitted.
- Empty result for a process → marked as **"Process not enabled"** (gray).

#### 2a. `trend.get` — fetch trend statistics (primary)

```json
{
  "method": "trend.get",
  "params": {
    "output": ["itemid", "clock", "value_min", "value_avg", "value_max"],
    "itemids": ["<itemid>"],
    "time_from": "<now - CHECKTRENDTIME>",
    "time_to": "<now>",
    "limit": 1
  }
}
```

- Returns the last trend record in the configured period.
- Result `[]` → triggers the fallback below.

#### 2b. `history.get` — fallback when trend is not available

```json
{
  "method": "history.get",
  "params": {
    "output": ["value"],
    "history": 0,
    "itemids": ["<itemid>"],
    "time_from": "<now - CHECKTRENDTIME>",
    "time_to": "<now>",
    "sortfield": "clock",
    "sortorder": "ASC",
    "limit": 2000
  }
}
```

- Used when the item has `trends=0` configured in Zabbix, or when the trend retention period has expired.
- The code collects up to 2,000 data points and calculates `min`, `avg` and `max` manually.
- Result still `[]` → process marked as **"Process not enabled"** (gray).

### Go function responsible

**File:** `cmd/app/main.go`  
**Function:** `generateZabbixReport(url, token string)` — the tab block starts around the line marked with `// --- Processos e Threads Zabbix Server ---`

#### Helpers used

| Function | Description |
|--------|-----------|
| `nameToWildcard(name)` | Converts `"agent poller"` → `"*agent*poller*"` for wildcard search |
| `wildcardMatch(pattern, key)` | Simple client-side match (`*`) to map returned items back to each name |
| `getProcessItemsBulk(apiUrl, token, names, hostid)` | Makes **1 `item.get`** with all patterns. Resolves collisions by specificity (more words = higher priority). Returns `map[nameLowercase]item` |
| `getLastTrend(apiUrl, token, itemid, days)` | Makes `trend.get` for the itemid in the configured period. Respects `CHECKTRENDTIME`. |
| `getHistoryStats(apiUrl, token, itemid, histType, days)` | Fallback: makes `history.get`, collects up to 2,000 data points and returns calculated `{value_min, value_avg, value_max}`. |

#### Version logic

The poller list varies based on the detected Zabbix version via `apiinfo.version`:

- **Zabbix ≥ 7:** includes `agent poller`, `browser poller`, `http agent poller`, `snmp poller`
- **Zabbix 6:** these four are displayed as **"Does not exist in this Zabbix version"**

#### Status logic

| Condition | Display |
|-----------|---------|
| `item.get` returns empty | Gray — "Process not enabled" |
| `trend.get` and `history.get` empty | Gray — "Process not enabled" |
| `value_avg < 50%` | Green — "OK" |
| `value_avg ≥ 50%` | Red — "Warning" |
| Error in any API call | "Error fetching data" |

#### Parallelism

Both pollers and internal processes are collected in **parallel goroutines**, controlled by a semaphore with capacity `MAX_CCONCURRENT`. Results are re-ordered by their original index to preserve display order, then re-ordered by `value_avg` descending (most utilized processes appear first).

### How to add a new process to the list

There are **3 places** in `cmd/app/main.go`:

**1. `procDesc`** — description shown in the `?` tooltip (key in lowercase):
```go
"new process": `"StartNewProcess" parameter: description and when to adjust.`,
```

**2. Correct table:**

- **Pollers (Data Collectors)** → `commonPollers` or `if majorV >= 7` block:
```go
commonPollers := []string{
    ...
    "new poller",
}
// or exclusive to Zabbix 7+:
if majorV >= 7 {
    pollerNames = append([]string{"new poller"}, pollerNames...)
}
```

- **Internal Process** → `procNames` slice:
```go
procNames := []string{
    ...
    "new process",
}
```

**3. Name rule:** use the name exactly as it appears in the Zabbix item key (with space or underscore). The `nameToWildcard` function converts automatically — `"agent poller"` → `"*agent*poller*"` — matching `agent poller`, `agent_poller` or any variant.

--- 

## Top Errors
  Checks the top item, trigger and LLD rule errors, ordered by number of failures.

  API call used to collect items with error (unsupported items):

  Parameters: item.get with
  output: ["itemid","name","templateid","error","key_"]
  filter: { "state": 1 }
  webitems: 1
  selectHosts: ["name","hostid"]
  inherited: true
  How errorCounter is filled:

  The code iterates each returned item, extracts templateid, name, error and host.
  Increments the errorCounter map with the key formed by itemError + "|" + tplId, where itemError is the error text and tplId is the template ID (or "no_template" if there is none).
  topErrors is iterated to generate the "Most Common Error Types" table.

---

These calls are made dynamically according to the Zabbix version and the environment data. Consult the code for details on optional parameters and fallback logic.

---

## Internationalization (i18n)

The interface and report support multiple languages. Currently available: **Portuguese (pt_BR)** — default language — and **English (en_US)**.

### Language selector

The selector is in the **header** of the page, to the right of the "ZBX-Easy" title. Switching the language translates the interface instantly (without reloading).

The preference is saved in the browser's `localStorage` under the key `zbx-lang` and automatically restored on future visits.

### How it works

| Component | File | Responsibility |
|---|---|---|
| Dictionaries | `app/web/locales/pt_BR/messages.json` and `app/web/locales/en_US/messages.json` | Contain all translations in the format `"key": "translated text"` |
| Runtime JS | `app/web/static/script.js` | Functions `t()`, `applyI18n()`, `setLang()` and `initI18n()` that load and apply translations |
| HTML template | `app/web/templates/index.html` | Uses attributes `data-i18n`, `data-i18n-placeholder`, `data-i18n-title` and `data-i18n-aria` |
| Backend Go | `app/cmd/app/main.go` | Emits HTML with `data-i18n` and `data-i18n-args` attributes for dynamic report texts |

### Translation attributes

The i18n system uses HTML attributes processed by JavaScript on the client side:

| Attribute | Target | Example |
|---|---|---|
| `data-i18n="key"` | Element `textContent` | `<span data-i18n="tabs.summary"></span>` |
| `data-i18n-args="val1\|val2"` | `%s` / `%d` arguments in the translation | `<span data-i18n="items.unsupported_paragraph" data-i18n-args="42\|3.50%"></span>` |
| `data-i18n-placeholder="key"` | Input `placeholder` | `<input data-i18n-placeholder="placeholder_url">` |
| `data-i18n-title="key"` | `title` (native tooltip) | `<div data-i18n-title="kpi.server_attention"></div>` |
| `data-i18n-aria="key"` | `aria-label` | `<button data-i18n-aria="aria_new_report"></button>` |

### Locale file structure

```
app/web/locales/
├── pt_BR/
│   └── messages.json      # ~290 keys — Portuguese (Brazil)
└── en_US/
    └── messages.json      # ~290 keys — English (US)
```

Each file is a flat JSON (`"key": "value"`). Keys with `%s` or `%d` accept positional arguments passed via `data-i18n-args`.

### Key categories

| Prefix | Content |
|---|---|
| `label_*`, `btn_*`, `placeholder_*`, `aria_*`, `tooltip_*` | Interface (form, buttons, fields) |
| `progress.*` | Progress messages during generation |
| `tabs.*` | Report tab names |
| `section.*` | Section titles (h3) in the report |
| `tip.*` | Explanatory tooltip texts (? icon) |
| `table.*` | Table headers |
| `summary.*` | Summary table rows |
| `gauge.*` | Doughnut chart captions |
| `proxy.*` | Proxy labels (summary, status) |
| `items.*`, `lld.*` | Item/LLD explanatory paragraphs |
| `types.*` | Item types (Zabbix Agent, SNMP, etc.) |
| `kpi.*` | KPIs in the Recommendations tab |
| `sub.*` | Subtitles of Recommendations sections |
| `rec.*` | Auxiliary Recommendations phrases |
| `procdesc.*` | Internal process descriptions (tooltips) |
| `badge.*` | Badges in recommendation cards |
| `error.*`, `stats.*`, `process.*` | Error and status messages |

### Loading flow

1. On page load, `initI18n()` reads `localStorage.zbx-lang` (fallback: `pt_BR`).
2. Makes `fetch('/locales/{lang}/messages.json?cb=<timestamp>')` — the `cb` parameter avoids caching.
3. Stores the dictionary in `_i18n` and calls `applyI18n()`.
4. `applyI18n()` iterates all elements with `data-i18n*` and replaces the content/attribute with the translation.
5. When the report is dynamically inserted (via AJAX), `applyI18n()` is called again to translate the HTML injected by the server.

### Server-side translation (Go)

The backend **does not** translate texts — it emits HTML with `data-i18n` markers that JavaScript translates on the client. Examples:

```go
// Section title with tooltip
html += titleWithInfo("h3", "i18n:section.pollers", "i18n:tip.pollers|15d")

// Numbered sub-section
html += nextSub(&sub, "i18n:sub.items_unsupported")

// Table with translatable headers
html += `<th data-i18n='table.description'></th>`

// Cell with arguments
html += `<span data-i18n='items.unsupported_paragraph' data-i18n-args='42|3.5%'></span>`
```

The helpers `titleWithInfo()` and `nextSub()` detect the `i18n:` prefix and automatically generate `data-i18n` and `data-i18n-args` attributes.

---

## Zabbix API calls used

Below are the main calls made by the Go backend to the Zabbix API to generate the report. Each call is made via JSON-RPC to the `/api_jsonrpc.php` endpoint of Zabbix.

### 1. apiinfo.version
- **Description:** Gets the Zabbix API version.
- **Parameters:** `[]` (empty)
- **Use:** Detect Zabbix version to adjust queries and links.

### 2. user.get
- **Description:** Fetches all registered users.
- **Parameters:** `{ "output": "userid" }`
- **Use:** Count the number of users.

### 3. item.get
- **Description:** Used in various contexts:
  - **Bulk fetch of all process items** (pollers + internal) in 1 call with wildcard
  - Fetch items by key (`key_`) and hostid
  - Count total, enabled, disabled, unsupported items
  - List unsupported items and their details
  - Fetch items without template
  - Fetch items by type (`type`), state (`state`), interval (`delay`)
- **Parameter examples:**
  - Bulk server process fetch (new approach):
    ```json
    {
      "output": ["itemid", "hostid", "name", "key_", "value_type"],
      "search": { "key_": ["*agent*poller*", "*history*syncer*", "*housekeeper*", "..."] },
      "searchByAny": true,
      "searchWildcardsEnabled": true,
      "hostids": "<ZABBIX_SERVER_HOSTID>"
    }
    ```
  - Fetch item by exact key:
    ```json
    { "output": ["itemid", "hostid", "name", "key_", "value_type"], "filter": {"key_": "zabbix[requiredperformance]"}, "hostids": "<hostid>", "limit": 1 }
    ```
  - Count unsupported items:
    ```json
    { "output": "extend", "filter": {"state": 1, "status": 0}, "monitored": true, "countOutput": true }
    ```
  - Count items by type:
    ```json
    { "output": "extend", "filter": {"type": 0}, "templated": false, "countOutput": true, "monitored": true }
    ```
  - Fetch items without template:
    ```json
    { "output": "extend", "filter": {"flags": 0}, "countOutput": true, "templated": false, "inherited": false }
    ```

### 4. history.get
- **Description:** Fetches the last history value of an item.
- **Parameters:**
  ```json
  { "output": "extend", "history": <value_type>, "itemids": "<itemid>", "sortfield": "clock", "sortorder": "DESC", "limit": 1 }
  ```
- **Use:** Get the most recent value of an item (e.g. NVPS).

### 5. trend.get
- **Description:** Fetches trend statistics (minimum, maximum, average) of an item over a time interval.
- **Parameters:**
  ```json
  { "output": ["itemid", "clock", "value_min", "value_avg", "value_max"], "itemids": ["<itemid>"], "limit": 1, "time_from": <unix>, "time_to": <unix> }
  ```
- **Use:** Usage statistics for processes/pollers.

### 6. host.get
- **Description:** Fetches registered hosts, enabled or disabled.
- **Parameters:**
  - All hosts: `{ "output": "hostid" }`
  - Enabled: `{ "output": "hostid", "filter": { "status": 0 } }`
  - Disabled: `{ "output": "hostid", "filter": { "status": 1 } }`

### 7. template.get
- **Description:** Fetches registered templates, by id or for counting.
- **Parameters:**
  - Count: `{ "countOutput": true }`
  - Fetch by id: `{ "output": ["templateid", "name"], "templateids": [<ids>] }`

### 8. discoveryrule.get
- **Description:** Fetches LLD (discovery) rules, by interval, state, etc.
- **Parameters:**
  - By interval:
    ```json
    { "output": "extend", "filter": {"delay": 60}, "templated": true, "countOutput": true }
    ```
  - Unsupported:
    ```json
    { "output": "extend", "filter": {"state": 1}, "templated": false, "countOutput": true }
    ```

### 9. proxy.get
- **Description:** Fetches registered proxies, by state (active/passive), status (online/offline), etc.
- **Parameters:**
  - All proxies: `{ "output": "proxyid" }`
  - Active: `{ "output": "proxyid", "filter": { "state": 2 } }`
  - Passive: `{ "output": "proxyid", "filter": { "state": 1 } }`
  - Online: `{ "output": "proxyid", "filter": { "status": 0 } }`
  - Offline: `{ "output": "proxyid", "filter": { "status": 1 } }`

---

These calls are made dynamically according to the Zabbix version and the environment data. Consult the code for details on optional parameters and fallback logic.

---

## Diagram: Zabbix API Calls

```mermaid
flowchart TD
  App["zabbix-easy app\n(app/cmd/app/main.go)"]

  subgraph ZabbixAPI["Zabbix API (JSON-RPC)"]
    HostGet["host.get\nfields: hostid, host, name, status, templates[]"]
    ProxyGet["proxy.get\nfields: proxyid, host, status, state"]
    ItemGetProxy["item.get (proxy scope)\nitemid, key_, hostid, lastvalue, lastclock, value_type, status"]
    ItemGetHost["item.get (host scope / type filters)\nitemid, key_, hostid, name, value_type"]
    ItemGetCount["item.get (countOutput:true)\nreturns count for KPIs"]
    TrendGet["trend.get\nitemid, clock, value_min, value_avg, value_max"]
    HistoryGet["history.get\nitemid, clock, value"]
    TemplateGet["template.get\ntemplateid, name, hosts"]
    UserGetCount["user.get (countOutput:true)\nreturns count"]
    DiscoveryGet["discoveryrule.get\ncount / details"]
  end

  subgraph Helpers["App helpers (functions)"]
    getItemByKey["getItemByKey()\nuses item.get"]
    getProcessBulk["getProcessItemsBulk()\nuses item.get with search wildcards"]
    getProxyProc["getProxyProcessItems()\nuses item.get type=5 (internal)"]
    getTrendsBulk["getTrendsBulkStats()\nuses trend.get"]
    getHistoryBulk["getHistoryStatsBulkByType()\nuses history.get"]
  end

  subgraph Tabs["UI Tabs / Views"]
    Summary["Summary tab"]
    Server["Server / Processes tab"]
    Proxies["Proxies tab"]
    Items["Items tab"]
    Templates["Templates tab"]
    Top["Top tab"]
    Recs["Recommendations tab"]
  end

  App --> HostGet
  App --> ProxyGet
  App --> ItemGetProxy
  App --> ItemGetHost
  App --> ItemGetCount
  App --> TrendGet
  App --> HistoryGet
  App --> TemplateGet
  App --> UserGetCount
  App --> DiscoveryGet

  getItemByKey --> ItemGetHost
  getProcessBulk --> ItemGetHost
  getProxyProc --> ItemGetHost
  getTrendsBulk --> TrendGet
  getHistoryBulk --> HistoryGet

  HostGet --> Summary
  UserGetCount --> Summary
  ItemGetCount --> Summary
  TemplateGet --> Summary
  ProxyGet --> Summary

  getProcessBulk --> Server
  getTrendsBulk --> Server
  getHistoryBulk --> Server
  ItemGetHost --> Server

  ProxyGet --> Proxies
  ItemGetProxy --> Proxies
  ItemGetCount --> Proxies
  getProxyProc --> Proxies
  HostGet --> Proxies

  ItemGetHost --> Items
  TemplateGet --> Items
  DiscoveryGet --> Items
  ItemGetCount --> Items

  TemplateGet --> Templates
  TemplateGet --> Top
  ItemGetCount --> Top

  getTrendsBulk --> Recs
  getHistoryBulk --> Recs
  ItemGetProxy --> Recs
  ItemGetHost --> Recs
  TemplateGet --> Recs
  DiscoveryGet --> Recs
  ProxyGet --> Recs
  ItemGetCount --> Recs

  classDef fix fill:#fff4a3,stroke:#e6c800
```
