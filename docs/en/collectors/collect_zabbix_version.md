---
title: "CollectZabbixVersion"
lang: en_US
---

# `CollectZabbixVersion`

Detects the Zabbix server version via `apiinfo.version` and returns a parsed major/minor version used across the application to select auth and field variations.

Usage

- Signature: `CollectZabbixVersion(apiUrl string, req ApiRequester) (string, error)`
- Zabbix method: `apiinfo.version`

Notes

- The collector is used at startup to set the `useBearerAuth` behavior (Bearer header for Zabbix >= 7.2) and to adapt proxy/field parsing between Zabbix 6 vs 7.
