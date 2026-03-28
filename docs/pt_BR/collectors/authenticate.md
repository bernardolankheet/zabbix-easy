---
title: "Authenticate"
lang: pt_BR
---

# `Authenticate`

Executa `user.login` e retorna o token de autenticação. Indicado para testes de credenciais e fluxos de login curtos.

Uso

- Assinatura: `Authenticate(apiUrl, username, password string, req ApiRequester) (string, error)`
- Método Zabbix: `user.login`
- Retorno: `token string` em caso de sucesso

Observações

- Use `Authenticate` para testes explícitos de usuário/senha (ex.: senha padrão Admin). Para operação normal, prefira usar o token obtido no login inicial e passá-lo para outros coletores.
