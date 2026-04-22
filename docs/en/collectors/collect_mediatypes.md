---
title: "CollectMediaTypes"
lang: en_US
---

# CollectMediaTypes

`CollectMediaTypes(apiUrl, token, req ApiRequester)` — returns the configured Zabbix media types.

Description

This collector calls `mediatype.get` with `output: ["mediatypeid","name"]` and returns an array of objects containing `mediatypeid` and `name`.

Usage

- Location: `app/internal/collector/collect_mediatypes.go`
- Zabbix method: `mediatype.get`
- Params: `output: ["mediatypeid","name"]`

Example (pseudo-result):

```
[ { "mediatypeid": "1", "name": "Email" }, { "mediatypeid": "0", "name": "Script" } ]
```

Notes

- `mediatypeid == 0` is treated as `Script` by the report for readability.
- This collector is used by the Alerts check to group failed alerts by media type and to surface the most common error message per media type/action.

Code reference

- `app/internal/collector/collect_mediatypes.go` — implemented using `CollectRawList`.
