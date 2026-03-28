---
title: "CollectHostExists"
lang: pt_BR
---

# `CollectHostExists`

Verifica se um host existe pelo `hostid`.

Uso

- Assinatura: `CollectHostExists(apiUrl, token, hostid string, req ApiRequester) (bool, error)`
- Chamadas: `host.get` com `filter:{hostid: <id>}`
