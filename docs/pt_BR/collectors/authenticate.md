---
title: "Authenticate"
lang: pt_BR
---

# `Authenticate`

Obtém um token de sessão via `user.login` quando nenhum token de API foi informado. É o caminho de autenticação por usuário e senha (issue #96), útil em frontends Zabbix somente-leitura, onde não é possível criar um token de API.

Uso

- Assinatura: `Authenticate(apiUrl, username, password string, req ApiRequester) (string, error)`
- Método Zabbix: `user.login`
- Parâmetros enviados: `{"username": ..., "password": ...}` (nomenclatura do Zabbix >= 6.0)

Observações

- A requisição é enviada **sem autenticação** — sem o campo `auth` e sem o header `Authorization` — pois é justamente a chamada que produz o token.
- O token retornado é usado exatamente como um token de API: no campo `auth` do JSON-RPC para Zabbix < 7.2, ou no header `Authorization: Bearer` para Zabbix >= 7.2.
- `generateZabbixReport` só chama esta função quando `token` está vazio, e executa `user.logout` (via `defer`) ao final do relatório, para não deixar sessões abertas.
- Um token vazio ou em branco na resposta é tratado como erro.
