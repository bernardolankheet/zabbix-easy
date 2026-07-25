---
title: "CollectServerHostId"
lang: en_US
---

# `CollectServerHostId`

Discovers the hostid of the host that carries the Zabbix Server's internal process items (`zabbix[process,...]`), so the report does not depend on a hardcoded id.

Usage

- Signature: `CollectServerHostId(apiUrl, token string, req ApiRequester) (string, error)`
- Zabbix method: `item.get` with `search: {key_: "zabbix[process,"}`, `monitored: true`, `selectHosts`

Notes

- Hostids are assigned per installation. The historical default `10084` is only the "Zabbix server" host on a **fresh** install; on an older or migrated one it points at some unrelated host and the whole Server tab comes out empty.
- Proxies also expose `zabbix[process,...]` items, so the winner is the host with the most **server-only** processes (escalator, alerter, alert manager, service manager, lld manager, report manager, proxy poller), falling back to the host with the most process items overall. Those names are stable across Zabbix 6.0–8.0.
- Returns `""` with no error when nothing matches — the caller keeps whatever `ZABBIX_SERVER_HOSTID` provided.
- `ZABBIX_SERVER_HOSTID` remains the explicit override and skips this call entirely. If the override matches no process item, detection runs as a fallback and its result wins.
