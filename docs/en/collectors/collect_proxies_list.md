---
title: "CollectProxiesList"
lang: en_US
---

# `CollectProxiesList`

Returns a lightweight list of proxies (IDs and counts) suitable for summary counts and fast lookups.

Usage

- Signature: `CollectProxiesList(apiUrl, token string, req ApiRequester) ([]map[string]interface{}, error)`
- Calls: `proxy.get` with minimal `output` fields
