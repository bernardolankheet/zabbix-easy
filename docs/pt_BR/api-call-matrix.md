---
title: "Matriz de Chamadas da API"
lang: pt_BR
---

# Matriz de Chamadas da API (métodos Zabbix → coletores → callers)

Esta matriz mapeia métodos JSON-RPC do Zabbix para os coletores que os encapsulam e os principais pontos de chamada no código. Use este arquivo como referência rápida para saber qual coletor usar e onde uma chamada da API é executada.

| Método Zabbix | Coletor | Principais callers | Propósito / parâmetros |
|---------------|---------|--------------------|------------------------|
| `apiinfo.version` | `CollectZabbixVersion(apiUrl, req)` | `app/cmd/app/main.go` (startup/geração de relatório) | Determina versão do Zabbix para habilitar Bearer auth (>=7.2) |
| `user.login` | `Authenticate(apiUrl, username, password, req)` | `app/cmd/app/main.go` (verificação Admin) | Teste para senha padrão Admin; retorna token |
| `item.get` | `CollectRawList(apiUrl, token, "item.get", params, req)`<br>`CollectItemByKey(apiUrl, token, key, hostid, req)`<br>`CollectProcessItemsBulk(apiUrl, token, names, hostid, req)`<br>`CollectProxyProcessItems(apiUrl, token, names, hostid, req)` | `app/cmd/app/main.go` (processos, itens, probes, SNMP, resolução de templates) | Busca de itens (filters, search wildcards, templated flag, campos de output) |
| `history.get` | `CollectRawList(apiUrl, token, "history.get", params, req)` | `app/cmd/app/main.go` (getLastHistoryValue, getHistoryStats, fallback bulk) | Obtém pontos brutos de histórico (fallback quando trends ausentes) |
| `trend.get` | `CollectRawList(apiUrl, token, "trend.get", params, req)` | `app/cmd/app/main.go` (getLastTrend, getTrendsBulkStats) | Obtém pontos de trend agregados (min/avg/max) no período |
| `host.get` | `CollectHosts(apiUrl, token, req)`<br>`CollectHostExists(apiUrl, token, hostid, req)` | `app/cmd/app/main.go` (resumo de hosts, verificações por host) | Busca hosts, contagens e existência |
| `template.get` | `CollectTemplates(apiUrl, token, req)` | `app/cmd/app/main.go` (contagem de templates, lookups) | Busca templates e metadados |
| `proxy.get` | `CollectProxies(apiUrl, token, majorV, req)`<br>`CollectProxiesList(apiUrl, token, req)` | `app/cmd/app/main.go` (resumo de proxies, análise por proxy) | Lista de proxies (diferenças v6/v7 tratadas no coletor) |
| `trigger.get` | `CollectTriggers(apiUrl, token, state, req)`<br>`CollectTriggersCount(apiUrl, token, req)` | `app/cmd/app/main.go` (aba Triggers e KPIs) | Coleta triggers filtrados por estado/templates |
| `user.get` | `CollectRawList(apiUrl, token, "user.get", params, req)`<br>`CollectCount(apiUrl, token, "user.get", nil, req)` | `app/cmd/app/main.go` (contagem de usuários, verificação Admin) | Listagem e contagem de usuários |

Notas
- Use os coletores em `app/internal/collector` em vez de chamar `zabbixApiRequest` diretamente — centraliza auth, transporte e parsing.
- Para métodos genéricos de lista usamos `CollectRawList` e para contagens `CollectCount` (usa `countOutput:true`).
- O mapeamento de processos (server/proxy) usa busca com wildcard + resolução client-side em `CollectProcessItemsBulk` e `CollectProxyProcessItems`.

Posso também gerar exemplos de entrada/saída para cada coletor se quiser aprofundar cada linha.
