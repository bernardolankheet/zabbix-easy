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

### GET /api/progress/{task_id}
Verifica o progresso da coleta.

### GET /api/report/{task_id}
Obtém o relatório gerado.
