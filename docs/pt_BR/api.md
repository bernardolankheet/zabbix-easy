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
  "zabbix_token": "Token de acesso"
}
```

**Validação:** `zabbix_url` e `zabbix_token` são obrigatórios e não podem ser vazios — a requisição retorna `400 Bad Request` se qualquer um estiver ausente. A URL pode ser fornecida como `http://host/` ou `http://host/api_jsonrpc.php`; ambos os formatos são aceitos.

### GET /api/progress/{task_id}
Verifica o progresso da coleta.

### GET /api/report/{task_id}
Obtém o relatório gerado.

