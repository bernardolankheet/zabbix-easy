---
title: "CollectItemByKey"
lang: en_US
---

# `CollectItemByKey`

Finds an item by exact `key_` value. Useful for locating NVPS and similar single-key items.

Usage

- Signature: `CollectItemByKey(apiUrl, token, key string, hostid string, req ApiRequester) (map[string]interface{}, error)`
- Zabbix method used: `item.get` (with filter on `key_`)
