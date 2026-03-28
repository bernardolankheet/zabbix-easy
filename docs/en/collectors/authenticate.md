---
title: "Authenticate"
lang: en_US
---

# `Authenticate`

Performs `user.login` and returns the auth token. Intended for credential checks and short-lived login flows.

Usage

- Signature: `Authenticate(apiUrl, username, password string, req ApiRequester) (string, error)`
- Zabbix method: `user.login`
- Returns: `token string` when successful

Notes

- Use `Authenticate` for explicit username/password checks (e.g., default Admin password test). For normal operation, prefer using the token obtained during the initial login and pass it to other collectors.

Example (Go)

```go
token, err := collector.Authenticate(apiUrl, "Admin", "zabbix", req)
if err != nil {
    return err
}
// use token for subsequent collectors
```
