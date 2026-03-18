# API

## Main Endpoints

### POST /api/start
Starts report collection.

**Body:**
```json
{
  "zabbix_url": "Zabbix URL",
  "zabbix_token": "Access token"
}
```

**Validation:** `zabbix_url` and `zabbix_token` are required — returns `400 Bad Request` if either is missing. The URL can be provided as `http://host/` or `http://host/api_jsonrpc.php`; both formats are accepted.

### GET /api/progress/{task_id}
Checks collection progress.

### GET /api/report/{task_id}
Retrieves the generated report.
