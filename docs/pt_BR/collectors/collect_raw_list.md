---
title: "CollectRawList"
lang: pt_BR
---

# `CollectRawList`

Helper genérico para métodos do Zabbix que retornam listas como `item.get`, `history.get` e `trend.get`.

Uso

- Assinatura: `CollectRawList(apiUrl, token, method string, params map[string]interface{}, req ApiRequester) (map[string]interface{}, error)`
- Métodos: `item.get`, `history.get`, `trend.get` (o caller fornece o `method`)
