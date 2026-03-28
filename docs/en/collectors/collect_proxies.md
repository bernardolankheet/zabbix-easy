---
title: "CollectProxies"
lang: en_US
---

# `CollectProxies` / `CollectProxiesList`

Lists proxies and normalizes differences between Zabbix 6 and 7 fields (state vs lastaccess, operating_mode vs status).

Usage

- `CollectProxies(apiUrl, token, majorV, req)` — returns extended proxy objects
- `CollectProxiesList(apiUrl, token, req)` — returns simple list for counts
