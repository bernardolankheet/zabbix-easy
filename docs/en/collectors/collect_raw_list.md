---
title: "CollectRawList"
lang: en_US
---

# `CollectRawList`

Generic helper for list-returning Zabbix methods such as `item.get`, `history.get` and `trend.get`.

Usage

- Signature: `CollectRawList(apiUrl, token, method string, params map[string]interface{}, req ApiRequester) (map[string]interface{}, error)`
- Methods: `item.get`, `history.get`, `trend.get` (caller provides `method`)

Notes

- Use for arbitrary list queries where typed collectors are not yet available.
- Returns the parsed JSON-RPC result as `map[string]interface{}` for caller-side processing.
