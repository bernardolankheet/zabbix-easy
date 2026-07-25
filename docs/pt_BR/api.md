---
title: "API"
lang: pt_BR
---

# API

## Endpoints principais

### POST /api/start
Inicia a coleta do relatório.

**Body:**
```json
{
  "zabbix_url": "URL do Zabbix",
  "zabbix_token": "Token de acesso",
  "zabbix_user": "Usuário (alternativa ao token)",
  "zabbix_password": "Senha (alternativa ao token)"
}
```

**Validação:** `zabbix_url` é obrigatório, e é preciso informar **um token de API** *ou* **usuário e senha**. Quando `zabbix_token` está vazio, a aplicação faz `user.login` com `zabbix_user`/`zabbix_password` para obter um token de sessão, e executa `user.logout` ao final do relatório. Útil em frontends somente-leitura, onde não é possível criar um token de API.

A URL pode ser fornecida como `http://host/` ou `http://host/api_jsonrpc.php`; ambos os formatos são aceitos.

### GET /api/progress/{task_id}
Verifica o progresso da coleta.

### GET /api/report/{task_id}
Obtém o relatório gerado.

