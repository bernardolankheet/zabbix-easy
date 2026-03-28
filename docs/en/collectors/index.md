---
title: "Collectors Index"
lang: en_US
---

# Collector Reference

This section documents the typed collectors in `app/internal/collector` used by the report generator. Each collector wraps one or more Zabbix JSON-RPC methods, centralizes authentication and parsing, and provides a small, well-typed API for callers.

Collectors included:

- [CollectZabbixVersion](collect_zabbix_version.md) — Zabbix version detection (`apiinfo.version`)
- [Authenticate](authenticate.md) — `user.login` helper
- [CollectRawList](collect_raw_list.md) — generic list-returning methods (`item.get`, `history.get`, `trend.get`)
- [CollectCount](collect_count.md) — `countOutput:true` queries
- [CollectItemByKey](collect_item_by_key.md) — find an item by exact `key_`
- [CollectProcessItemsBulk](collect_process_items_bulk.md) — bulk process item discovery (server)
- [CollectProxyProcessItems](collect_proxy_process_items.md) — bulk process item discovery (proxy)
- [CollectHosts](collect_hosts.md) / [CollectHostExists](collect_host_exists.md) — host list and existence checks
- [CollectTemplates](collect_templates.md) — templates listing
- [CollectProxies](collect_proxies.md) / [CollectProxiesList](collect_proxies_list.md) — proxies listing
- [CollectTriggers](collect_triggers.md) / [CollectTriggersCount](collect_triggers.md) — triggers listing and counts

Click any collector name above to open its documentation and examples.
