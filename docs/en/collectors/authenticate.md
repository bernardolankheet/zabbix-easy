---
title: "Authenticate"
lang: en_US
---

# `Authenticate`

Obtains a session token from `user.login` when no API token was supplied. This is the username/password authentication path (issue #96), useful on read-only Zabbix frontends where an API token cannot be created.

Usage

- Signature: `Authenticate(apiUrl, username, password string, req ApiRequester) (string, error)`
- Zabbix method: `user.login`
- Parameters sent: `{"username": ..., "password": ...}` (Zabbix >= 6.0 naming)

Notes

- The request is sent **unauthenticated** — no `auth` field and no `Authorization` header — since it is the call that produces the token.
- The returned token is used exactly like an API token afterwards: in the JSON-RPC `auth` field for Zabbix < 7.2, or in the `Authorization: Bearer` header for Zabbix >= 7.2.
- `generateZabbixReport` calls this only when `token` is empty, and issues a `user.logout` (via `defer`) once the report finishes, so sessions are not left behind.
- An empty or blank token in the response is treated as an error.
