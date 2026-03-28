---
title: "CollectProcessItemsBulk"
lang: pt_BR
---

# `CollectProcessItemsBulk`

Descobre itens de processos do servidor usando buscas com wildcard e resolve colisões client-side. Retorna um mapa de nome-do-processo → metadados do item.

Uso

- Assinatura: `CollectProcessItemsBulk(apiUrl, token string, names []string, hostid string, req ApiRequester) (map[string]map[string]interface{}, error)`
