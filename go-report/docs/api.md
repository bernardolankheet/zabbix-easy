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

---

## Chamadas à API do Zabbix utilizadas

Abaixo estão listadas as principais chamadas feitas pelo backend Go à API do Zabbix para gerar o relatório. Cada chamada é feita via JSON-RPC para o endpoint `/api_jsonrpc.php` do Zabbix.

### 1. apiinfo.version
- **Descrição:** Obtém a versão da API do Zabbix.
- **Parâmetros:** `[]` (vazio)
- **Uso:** Detectar versão do Zabbix para ajustar queries e links.

### 2. user.get
- **Descrição:** Busca todos os usuários cadastrados.
- **Parâmetros:** `{ "output": "userid" }`
- **Uso:** Contar número de usuários.

### 3. item.get
- **Descrição:** Utilizada em diversos contextos:
  - Buscar itens por chave (`key_`) e hostid
  - Contar itens totais, habilitados, desabilitados, não suportados
  - Listar itens não suportados e seus detalhes
  - Buscar itens sem template
  - Buscar itens por tipo (`type`), estado (`state`), intervalo (`delay`)
- **Exemplos de parâmetros:**
  - Buscar item por chave:
    ```json
    { "output": ["itemid", "hostid", "name", "key_", "value_type"], "filter": {"key_": "zabbix[requiredperformance]"}, "hostids": "<hostid>", "limit": 1 }
    ```
  - Contar itens não suportados:
    ```json
    { "output": "extend", "filter": {"state": 1, "status": 0}, "monitored": true, "countOutput": true }
    ```
  - Contar itens por tipo:
    ```json
    { "output": "extend", "filter": {"type": 0}, "templated": false, "countOutput": true, "monitored": true }
    ```
  - Buscar itens sem template:
    ```json
    { "output": "extend", "filter": {"flags": 0}, "countOutput": true, "templated": false, "inherited": false }
    ```

### 4. history.get
- **Descrição:** Busca o último valor de histórico de um item.
- **Parâmetros:**
  ```json
  { "output": "extend", "history": <value_type>, "itemids": "<itemid>", "sortfield": "clock", "sortorder": "DESC", "limit": 1 }
  ```
- **Uso:** Obter valor mais recente de um item (ex: NVPS).

### 5. trend.get
- **Descrição:** Busca estatísticas de tendência (mínimo, máximo, média) de um item em um intervalo de tempo.
- **Parâmetros:**
  ```json
  { "output": ["itemid", "clock", "value_min", "value_avg", "value_max"], "itemids": ["<itemid>"], "limit": 1, "time_from": <unix>, "time_to": <unix> }
  ```
- **Uso:** Estatísticas de uso de processos/pollers.

### 6. host.get
- **Descrição:** Busca hosts cadastrados, habilitados ou desabilitados.
- **Parâmetros:**
  - Todos os hosts: `{ "output": "hostid" }`
  - Habilitados: `{ "output": "hostid", "filter": { "status": 0 } }`
  - Desabilitados: `{ "output": "hostid", "filter": { "status": 1 } }`

### 7. template.get
- **Descrição:** Busca templates cadastrados, por id ou para contagem.
- **Parâmetros:**
  - Contar: `{ "countOutput": true }`
  - Buscar por id: `{ "output": ["templateid", "name"], "templateids": [<ids>] }`

### 8. discoveryrule.get
- **Descrição:** Busca regras de LLD (descoberta), por intervalo, estado, etc.
- **Parâmetros:**
  - Por intervalo:
    ```json
    { "output": "extend", "filter": {"delay": 60}, "templated": true, "countOutput": true }
    ```
  - Não suportadas:
    ```json
    { "output": "extend", "filter": {"state": 1}, "templated": false, "countOutput": true }
    ```

---

Essas chamadas são feitas dinamicamente conforme a versão do Zabbix e os dados do ambiente. Consulte o código para detalhes de parâmetros opcionais e lógica de fallback.
