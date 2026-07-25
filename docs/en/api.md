---
title: "API"
lang: en_US
---

# API

## Main Endpoints

### POST /api/start
Starts report collection.

**Body:**
```json
{
  "zabbix_url": "Zabbix URL",
  "zabbix_token": "Access token",
  "zabbix_user": "Username (alternative to the token)",
  "zabbix_password": "Password (alternative to the token)"
}
```

**Validation:** `zabbix_url` is required, and you must supply either **an API token** *or* **a username and password**. When `zabbix_token` is empty the app performs a `user.login` with `zabbix_user`/`zabbix_password` to obtain a session token, and calls `user.logout` once the report is done. Useful on read-only frontends where an API token cannot be created.

The URL can be supplied as `http://host/` or `http://host/api_jsonrpc.php`; both are accepted.

### GET /api/progress/{task_id}
Checks collection progress.

### GET /api/report/{task_id}
Retrieves the generated report.

