---
title: "CollectProcessItemsBulk"
lang: en_US
---

# `CollectProcessItemsBulk`

Discovers server-side process items using wildcard search and resolves name collisions client-side. Returns a map of process-name → item metadata.

Usage

- Signature: `CollectProcessItemsBulk(apiUrl, token string, names []string, hostid string, req ApiRequester) (map[string]map[string]interface{}, error)`
- Calls: `item.get` (search with wildcards), then `trend.get` / `history.get` for stats
