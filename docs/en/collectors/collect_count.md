---
title: "CollectCount"
lang: en_US
---

# `CollectCount`

Helper for `countOutput:true` queries (returns numeric counts instead of full lists).

Usage

- Signature: `CollectCount(apiUrl, token, method string, params map[string]interface{}, req ApiRequester) (int, error)`
- Typical methods: `item.get` with `countOutput:true`, `user.get` with `countOutput:true`
