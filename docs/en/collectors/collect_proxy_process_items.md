---
title: "CollectProxyProcessItems"
lang: en_US
---

# `CollectProxyProcessItems`

Proxy-specific process item discovery and resolution. Similar to the server bulk collector but tuned for proxy item types and proxyids.

Usage

- Signature: `CollectProxyProcessItems(apiUrl, token string, names []string, hostid string, req ApiRequester) (map[string]map[string]interface{}, error)`
- Calls: `item.get`, `trend.get`, `history.get` as needed
