---
title: "CollectZabbixVersion"
lang: pt_BR
---

# `CollectZabbixVersion`

Detecta a versão do Zabbix via `apiinfo.version` e retorna a versão usada pela aplicação para selecionar auth e variações de campos.

Uso

- Assinatura: `CollectZabbixVersion(apiUrl string, req ApiRequester) (string, error)`
- Método Zabbix: `apiinfo.version`

Observações

- Usado na inicialização para definir `useBearerAuth` (Bearer header quando Zabbix >= 7.2) e para adaptar parsing entre Zabbix 6 e 7.
