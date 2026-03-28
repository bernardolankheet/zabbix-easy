---
title: "CollectHostExists"
lang: en_US
---

# `CollectHostExists`

Checks whether a host exists by `hostid`.

Usage

- Signature: `CollectHostExists(apiUrl, token, hostid string, req ApiRequester) (bool, error)`
- Calls: `host.get` with `filter:{hostid: <id>}`
