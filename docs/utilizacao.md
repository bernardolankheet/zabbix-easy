# Utilização

1. Acesse a interface web
2. Informe a URL e o token do Zabbix
3. Aguarde a geração do relatório
4. Exporte ou imprima o relatório conforme necessário

---

## Variáveis de ambiente globais

Estas variáveis afetam o comportamento de toda a geração do relatório:

| Variável              | Padrão  | Descrição                                                                                   |
|-----------------------|---------|---------------------------------------------------------------------------------------------|
| `ZABBIX_SERVER_HOSTID`| _(vazio)_ | ID do host do Zabbix Server. Usado para filtrar chamadas de item por host. Se não definido, a busca ocorre sem filtro de host. |
| `CHECKTRENDTIME`      | `30d`   | Janela de tempo para análise de trends/histórico. Aceita sufixo `d` (dias), `h` (horas), `m` (minutos). Ex: `7d`, `24h`. |
| `MAX_CCONCURRENT`     | `6`     | Número máximo de goroutines paralelas fazendo chamadas à API do Zabbix simultaneamente.      |
| `APP_DEBUG`           | _(vazio)_ | `1` ou `true` para ativar logs detalhados de cada requisição/resposta da API Zabbix.        |

---

## Fluxo geral de geração

A função principal é `generateZabbixReport(url, token string)` em `cmd/app/main.go`.

```
POST /api/start
  → cria Task em memória → goroutine: generateZabbixReport()
      → progressCb() atualiza mensagem de progresso
      → retorna HTML fragment
      → salva no PostgreSQL (se DB_HOST configurado)

GET /api/progress/:id      → polling de status + mensagem de progresso
GET /api/report/:id        → retorna o HTML fragment gerado (sessão atual)
GET /api/reportdb/:id      → retorna relatório salvo no banco
GET /api/reportdb/:id?raw=1 → retorna fragment bare para renderização inline
```

A geração detecta a versão do Zabbix via `apiinfo.version` e ajusta chamadas e listas de processos automaticamente para Zabbix 6 e 7.

---

## Guias do Relatório

O relatório é dividido em 7 guias. A seguir a documentação completa de cada uma.

---

## Guia 1: Resumo do Ambiente (`tab-resumo`)

### O que é

Visão consolidada do ambiente Zabbix com os principais contadores. É a guia exibida por padrão ao carregar o relatório. Inclui dois gráficos doughnut (gauge) para visualização rápida de hosts desabilitados e itens não suportados.

### Tabela exibida

| Parâmetro | Valor | Detalhes |
|-----------|-------|----------|
| Número de hosts | total | habilitados / desabilitados |
| Número de templates | contagem | — |
| Número de itens | total | habilitados / desabilitados / não suportados |
| Número de Proxys | contagem | — |
| Número de usuários | contagem | — |
| Required server performance (NVPS) | float | new values per second |

**Gauges:**
- **Hosts Desabilitados** — doughnut com proporção habilitados vs desabilitados
- **Itens Não Suportados** — doughnut com proporção suportados vs não suportados

### Chamadas à API do Zabbix

| Chamada | Parâmetros relevantes | Dado extraído |
|---------|----------------------|---------------|
| `apiinfo.version` | _(sem auth)_ | Versão do Zabbix; determina o `majorV` usado em todo o relatório |
| `item.get` | `filter:{state:1,status:0}, monitored:true, countOutput:true` | Total de itens não suportados |
| `user.get` | `output:"userid"` | Contagem de usuários |
| `item.get` | `filter:{key_:"zabbix[requiredperformance]"}, hostids:<ZABBIX_SERVER_HOSTID>` | Localiza o item de NVPS |
| `history.get` | `itemids:<id>, sortorder:DESC, limit:1` | Último valor do item NVPS |
| `host.get` | `output:"hostid"` | Total de hosts |
| `host.get` | `filter:{status:0}` | Hosts habilitados |
| `host.get` | `filter:{status:1}` | Hosts desabilitados |
| `template.get` | `countOutput:true` | Total de templates |
| `item.get` | `countOutput:true, templated:false, webitems:true` | Total de itens |
| `item.get` | `countOutput:true, monitored:true, filter:{status:0,state:0}` | Itens habilitados |
| `item.get` | `countOutput:true, filter:{status:1}` | Itens desabilitados |
| `proxy.get` | `countOutput:true` | Total de proxies |
| `proxy.get` | `output:extend` | Lista completa de proxies (usada em outras guias) |

### Funções Go responsáveis

| Função | Descrição |
|--------|-----------|
| `getItemByKey(apiUrl, token, "zabbix[requiredperformance]", hostid)` | Localiza o item NVPS com cache em memória |
| `getLastHistoryValue(apiUrl, token, itemid, histType)` | Busca o último valor do history para o NVPS |
| `getProxyCount(apiUrl, token)` | Retorna `countOutput` de `proxy.get` |
| `getProxies(apiUrl, token)` | Retorna lista completa de proxies |

---

## Guia 2: Zabbix Server (`tab-processos`)

### O que é

Exibe o nível de utilização dos processos internos do Zabbix Server, divididos em dois grupos:

- **Pollers (Data Collectors):** processos que coletam métricas ativamente (`poller`, `http poller`, `icmp pinger`, `agent poller`, `snmp poller`, etc.)
- **Internal Processes:** processos de infraestrutura do servidor (`history syncer`, `housekeeper`, `escalator`, `trapper`, `lld manager`, etc.)

Para cada processo é exibido `min`, `avg` e `max` de utilização (%), além de status visual **OK** (avg < 50%) ou **Atenção** (avg ≥ 50%).

### Tabela exibida

| Coluna | Descrição |
|--------|-----------|
| Poller / Processo | Nome com ícone `?` de tooltip com descrição do parâmetro `zabbix_server.conf` |
| value_min | Mínimo de utilização no período (`CHECKTRENDTIME`) |
| value_avg | Média de utilização no período |
| value_max | Pico de utilização no período |
| Status | Verde OK / Vermelho Atenção / Cinza não habilitado |

### Chamadas à API do Zabbix

#### 1. `item.get` — busca bulk de todos os processos (1 chamada única)

Antes de disparar as goroutines de trend/history, o código faz **uma única chamada** `item.get` que cobre todos os processos (pollers + internal) de uma vez:

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid", "name", "key_", "value_type"],
    "search": {
      "key_": [
        "*agent*poller*",
        "*browser*poller*",
        "*configuration*syncer*",
        "*history*syncer*",
        "*housekeeper*",
        "*poller*",
        "..."
      ]
    },
    "searchByAny": true,
    "searchWildcardsEnabled": true,
    "hostids": "<ZABBIX_SERVER_HOSTID>"
  }
}
```

- Cada nome de processo é convertido para um padrão wildcard por `nameToWildcard`: `"agent poller"` → `"*agent*poller*"`. Isso funciona tanto com chaves `agent poller` quanto `agent_poller`.
- `searchByAny: true` faz o Zabbix retornar qualquer item que case com **ao menos um** padrão.
- Os items retornados são mapeados de volta a cada nome via `wildcardMatch` (client-side), priorizando o padrão mais específico (mais palavras) para evitar colisões entre `*poller*` e `*agent*poller*`.
- Se `ZABBIX_SERVER_HOSTID` não estiver definida, o parâmetro `hostids` é omitido.

#### 2a. `trend.get` — estatísticas do período (primária)

```json
{
  "method": "trend.get",
  "params": {
    "output": ["itemid", "clock", "value_min", "value_avg", "value_max"],
    "itemids": ["<itemid>"],
    "time_from": "<agora - CHECKTRENDTIME>",
    "time_to": "<agora>",
    "limit": 1
  }
}
```

#### 2b. `history.get` — fallback quando `trends=0` no item

```json
{
  "method": "history.get",
  "params": {
    "output": ["value"],
    "history": 0,
    "itemids": ["<itemid>"],
    "time_from": "<agora - CHECKTRENDTIME>",
    "time_to": "<agora>",
    "sortorder": "ASC",
    "limit": 2000
  }
}
```

Quando o `trend.get` retorna vazio (item com `trends=0` ou retenção expirada), o código coleta até 2.000 pontos do histórico e calcula `min/avg/max` manualmente.

### Funções Go responsáveis

| Função | Descrição |
|--------|-----------|
| `nameToWildcard(name)` | Converte `"agent poller"` → `"*agent*poller*"` para a busca wildcard |
| `wildcardMatch(pattern, key)` | Match client-side simples (`*`) para mapear items retornados de volta a cada nome de processo |
| `getProcessItemsBulk(apiUrl, token, names, hostid)` | Faz **1 `item.get`** com todos os padrões. Resolve colisões por especificidade (mais palavras = prioridade maior). Retorna `map[nomeLowercase]item` |
| `getLastTrend(apiUrl, token, itemid, days)` | `trend.get` no período configurado |
| `getHistoryStats(apiUrl, token, itemid, histType, days)` | Fallback: `history.get` + cálculo manual de min/avg/max |

### Lógica de versão

| Zabbix | Pollers extras |
|--------|---------------|
| ≥ 7 | Inclui `agent poller`, `browser poller`, `http agent poller`, `snmp poller` |
| 6 | Esses quatro aparecem como "Não existe nesta versão do Zabbix" |

### Lógica de status

| Condição | Exibição |
|----------|----------|
| `item.get` retorna vazio | Cinza — "Processo não habilitado" |
| `trend.get` e `history.get` vazios | Cinza — "Processo não habilitado" |
| `ZABBIX_SERVER_HOSTID` definido mas hostid inválido | Cinza — "Hostid X não encontrado" |
| avg < 50% | Verde — OK |
| avg ≥ 50% | Vermelho — Atenção |
| Erro em qualquer chamada | "Erro ao obter dados" |

### Como adicionar um novo processo

São **3 lugares** em `cmd/app/main.go`:

**1. `procDesc`** — descrição exibida no tooltip `?` (obrigatório, chave em lowercase):
```go
"novo processo": `Parâmetro "StartNovoProcesso": descrição e quando ajustar.`,
```

**2. Tabela correta:**

- **Pollers (Data Collectors)** → adicione em `commonPollers` ou dentro de `if majorV >= 7`:
```go
commonPollers := []string{
    ...
    "novo poller",  // ← aqui
}
// ou exclusivo do Zabbix 7+:
if majorV >= 7 {
    pollerNames = append([]string{"novo poller"}, pollerNames...)
}
```

- **Internal Process** → adicione em `procNames`:
```go
procNames := []string{
    ...
    "novo processo",  // ← aqui
}
```

**3. Regra do nome:** use o nome exatamente como aparece na chave do item no Zabbix, com espaço ou underscore. A função `nameToWildcard` converte automaticamente — `"agent poller"` → `"*agent*poller*"` — e casa com `agent poller`, `agent_poller` ou qualquer variante.

---

## Guia 3: Zabbix Proxys (`tab-proxys`)

### O que é

Exibe o status e métricas dos Zabbix Proxies configurados no ambiente. Divide os proxies em: Unknown, Offline, Ativos (Active), Passivos (Passive). Para cada proxy ativo/comunicando exibe itens totais, não suportados e fila de 10 minutos.

### Tabelas exibidas

**Sumário:**

| Descrição | Quantidade |
|-----------|-----------|
| Proxys Unknown | contagem |
| Proxys Offline | contagem |
| Proxys Ativos | contagem |
| Proxys Passivos | contagem |
| Total de Proxys | contagem + link |

**Detalhe por proxy** (somente proxies com `state=2`, comunicando):

| Proxy | Tipo | Total de Itens | Items não suportados | Queue-10m |
|-------|------|----------------|----------------------|-----------|
| nome | Active / Passive | contagem | contagem | valor |

### Chamadas à API do Zabbix

A lista de proxies já foi coletada no início (Resumo). Por proxy ativo, são feitas duas chamadas paralelas:

| Chamada | Parâmetros relevantes | Dado extraído |
|---------|-----------------------|---------------|
| `item.get` | `search:{key_:["*queue,10m*","*items_unsupported*", ...]}, proxyids:<id>, monitored:true` | `lastvalue` de `zabbix[queue,10m]` e `zabbix[items_unsupported]` |
| `item.get` | `countOutput:true, templated:false, proxyids:<id>` | Total de itens monitorados pelo proxy |

### Lógica de versão

| Campo | Zabbix ≥ 7 | Zabbix 6 |
|-------|-----------|---------|
| Tipo (Active/Passive) | `operating_mode` (`0`=Active, `1`=Passive) | `status` (`5`=Active, `6`=Passive) |
| Estado (Unknown/Offline) | `state` (`0`=Unknown, `1`=Offline, `2`=Online) | `state` ou `status` como fallback |

### Como funciona no código

As linhas por proxy são geradas em goroutines paralelas com o semáforo `sem`. Os resultados são reordenados pelo índice original para manter a ordem de exibição.

---

### Processos e Threads dos Proxys

#### O que é

Exibe a utilização dos processos internos de cada Zabbix Proxy em um accordion por proxy. Para cada processo é mostrado `min`, `avg` e `max` de utilização (%), além de badge **OK** ou **Atenção** no cabeçalho do accordion.

#### Tabela exibida (uma por proxy)

| Coluna | Descrição |
|--------|-----------|
| Processo | Nome com ícone `?` de tooltip com descrição do parâmetro `zabbix_proxy.conf` |
| value_min | Mínimo de utilização no período (`CHECKTRENDTIME`) |
| value_avg | Média de utilização no período |
| value_max | Pico de utilização no período |
| Status | Verde OK / Vermelho Atenção / Cinza não habilitado / Cinza sem dados |

O accordion de cada proxy exibe dois badges no cabeçalho:

- **Online / Offline·Unknown** — estado atual de comunicação com o Zabbix Server
- **OK / Atenção** — pior `value_avg` entre todos os processos com dados

#### Chamadas à API do Zabbix

Cada proxy ativo executa um goroutine independente (controlado pelo semáforo `sem`) com o fluxo a seguir:

##### Step 1 — Descoberta do hostid do proxy (3 tentativas)

O hostid do host de auto-monitoramento do proxy pode diferir do proxyid a partir do Zabbix 7.

| Tentativa | Método | Parâmetros | Quando funciona |
|-----------|--------|-----------|-----------------|
| A | `host.get` | `hostids: [proxyid]` | Zabbix 6 — proxyid == hostid |
| B | `host.get` | `filter: {host: proxyName}` | Zabbix 7 — busca por nome técnico |
| C | `host.get` | `filter: {name: proxyName}` | Zabbix 7 — busca por nome de exibição |
| Fallback | — | usa `proxyid` diretamente | último recurso |

##### Step 2 — `item.get` bulk (1 chamada, todos os processos do proxy)

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid", "name", "key_", "value_type"],
    "hostids": "<hostid resolvido no Step 1>",
    "filter": { "type": 5 }
  }
}
```

- Busca **todos** os itens do tipo `5` (Zabbix internal) do host — sem wildcard na API.
- O match é feito **client-side**: para cada item retornado, testa o padrão `nameToWildcard` contra **`key_` e `name`** do item.
- Checar o campo `name` garante compatibilidade entre Zabbix 6 e 7, já que o `name` ("Utilization of data sender processes, in %") é estável mesmo quando a `key_` muda de formato.

##### Step 3 — `trend.get` bulk (1 chamada, todos os itens do proxy)

```json
{
  "method": "trend.get",
  "params": {
    "output": ["itemid", "value_min", "value_avg", "value_max"],
    "itemids": ["<iid1>", "<iid2>", "..."],
    "time_from": "<agora - CHECKTRENDTIME>",
    "time_to": "<agora>"
  }
}
```

- Agrega múltiplos registros de trend por item: `min(value_min)`, `mean(value_avg)`, `max(value_max)`.
- Itens sem dados no resultado disparam o fallback abaixo.

##### Step 4 — `history.get` fallback (1 chamada por `value_type`, somente itens sem trend)

```json
{
  "method": "history.get",
  "params": {
    "output": ["itemid", "value"],
    "history": "<value_type>",
    "itemids": ["<iids sem trend>"],
    "time_from": "<agora - CHECKTRENDTIME>",
    "time_to": "<agora>",
    "sortfield": "clock",
    "sortorder": "ASC",
    "limit": 20000
  }
}
```

- Agrupa os itens pelo `value_type` e faz uma chamada por tipo (máximo de 20.000 linhas por chamada).
- Calcula `min/avg/max` manualmente a partir dos valores brutos.

#### Funções Go responsáveis

| Função | Descrição |
|--------|-----------|
| `getProxies(apiUrl, token)` | Retorna lista completa de proxies com todos os campos (`output:extend`) |
| `getProxyProcessItems(apiUrl, token, names, hostid)` | Busca todos os itens `type=5` do host; match client-side em `key_` **e** `name` usando `nameToWildcard` |
| `getTrendsBulkStats(apiUrl, token, itemids)` | **1 `trend.get`** para todos os itemids; agrega `min/avg/max` por item |
| `getHistoryStatsBulkByType(apiUrl, token, items)` | Fallback: **1 `history.get` por `value_type`**; agrega `min/avg/max` a partir do histórico bruto |
| `nameToWildcard(name)` | Converte `"data*sender"` → `"*data*sender*"` para match client-side |
| `wildcardMatch(pattern, s)` | Match simples com `*`; usado por `getProxyProcessItems` para testar `key_` e `name` |

#### Lógica de versão

| Zabbix | Processos extras |
|--------|----------------|
| ≥ 7 | Inclui `agent poller`, `browser poller`, `http agent poller`, `snmp poller` |
| 6 | Esses quatro são ignorados na construção da tabela |

#### Lógica de status

| Condição | Exibição |
|----------|----------|
| Proxy offline / unknown | Accordion sem tabela; badge **Offline/Unknown** |
| Nenhum item `type=5` encontrado | Nota com o hostid usado; sem tabela |
| `trend.get` e `history.get` sem dados | Cinza — "Sem dados" |
| Item não encontrado para o processo | Cinza — "Processo não habilitado" |
| `value_avg < 60%` | Verde — OK |
| `value_avg ≥ 60%` | Vermelho — Atenção |

#### Como adicionar um novo processo ao proxy

São **2 lugares** em `cmd/app/main.go`:

**1. `procDesc`** — descrição do tooltip `?` (chave em lowercase, com espaços em vez de `*`):
```go
"novo processo": `Parâmetro "StartNovoProcesso": descrição e quando ajustar.`,
```

**2. `proxyAllProcNames`** — lista de processos do proxy (use `*` como separador de palavras):
```go
proxyAllProcNames := []string{
    "data*sender",
    ...
    "novo*processo",  // ← aqui
}
// ou exclusivo do Zabbix 7+:
if majorV >= 7 {
    proxyAllProcNames = append([]string{"novo*processo"}, proxyAllProcNames...)
}
```

**Regra do `*`:** use `*` entre as palavras do nome (ex: `"data*sender"`, `"lld*manager"`). O `*` funciona como separador wildcard, permitindo casar tanto `data sender` quanto `data_sender` na `key_` e no `name` do item.

---

## Guia 4: Items e LLDs (`tab-items`)

### O que é

Análise detalhada de itens monitorados e regras de descoberta (LLD). Está dividida em cinco seções:

1. **Items sem Template** — itens criados diretamente no host, fora de templates
2. **Itens não suportados** — breakdown por tipo de item (Zabbix Agent, SNMP, HTTP, etc.)
3. **Intervalo de Coleta** — itens com delay de 1s, 10s, 30s, 60s
4. **Regras de LLD — Intervalo de Coleta** — discovery rules com delay de 1s, 10s, 30s, 60s, 300s
5. **Items Texto com Histórico** — itens do tipo Texto com history retido e delay ≤ 300s

### Tabelas exibidas

**Items sem Template:**

| Descrição | Quantidade | Link |
|-----------|-----------|------|
| Itens sem Template | contagem | link filtrado |

**Itens não suportados (por tipo):**

| Tipo de Item | Total | Não suportados | Link |
|-------------|-------|---------------|------|
| Zabbix Agent | n | n | link |
| SNMP | n | n | link |
| … | … | … | … |

**Intervalo de Coleta / LLD:**

| Intervalo (s) | Quantidade | Link |
|--------------|-----------|------|
| 1 | n | link |
| 10 | n | link |

**Items Texto com Histórico:**

| Template | Nome do Item | ItemID | Intervalo (s) | Link |
|----------|-------------|--------|--------------|------|

### Chamadas à API do Zabbix

| Chamada | Parâmetros relevantes | Dado extraído |
|---------|-----------------------|---------------|
| `item.get` | `filter:{flags:0}, inherited:false, templated:false, countOutput:true` | Items sem template |
| `item.get` | `filter:{type:<code>}, countOutput:true, monitored:true` | Total por tipo de item |
| `item.get` | `filter:{type:<code>,state:1}, countOutput:true, monitored:true` | Não suportados por tipo |
| `item.get` | `filter:{delay:<1\|10\|30\|60>}, countOutput:true` | Itens por intervalo de coleta |
| `discoveryrule.get` | `filter:{delay:<1\|10\|30\|60\|300>}, countOutput:true, templated:true` | LLD rules por intervalo |
| `discoveryrule.get` | `filter:{state:1}, countOutput:true, templated:false` | LLD rules não suportadas |
| `item.get` | `templated:true, filter:{value_type:4, delay:[30,60,120,300], history:["1h","1d","7d","31d"]}, selectHosts:["hostid"]` | Items texto com histórico e delay curto |
| `template.get` | `filter:{hostid:<ids>}, selectHosts:["hostid"]` | Resolve nomes de templates para os items texto |

### Lógica de versão

- **Browser (type=22):** incluído na tabela de não suportados apenas para Zabbix ≥ 7
- **Links do frontend:** `zabbix.php?action=item.list` (Zabbix 7) ou `items.php` (Zabbix 6)
- **Links LLD:** `host_discovery.php` com parâmetros adaptados por versão; delay formatado como `Xs` ou `Xm`

### Paralelismo

As chamadas de `item.get` por tipo (total + não suportados) são executadas em goroutines paralelas controladas pelo semáforo `sem`. As linhas são reordenadas por `Unsup desc` para colocar os tipos mais problemáticos primeiro.

---

## Guia 5: Templates (`tab-templates`)

### O que é

Detalhamento dos **Top N templates** com mais itens não suportados. Para cada template exibe a lista dos itens problemáticos com link direto de edição no frontend do Zabbix.

### Tabela exibida (uma por template)

| Item | Erro | Host | Link |
|------|------|------|------|
| nome do item | mensagem de erro | hostname | [Editar] |

### Chamadas à API do Zabbix

**Nenhuma chamada nova.** Todos os dados desta guia são calculados a partir do resultado do `item.get` com `state:1, inherited:true` coletado na fase inicial (mesmos dados usados pela guia Top Hosts/Templates/Itens).

O ranking de templates é construído assim:
1. Para cada item não suportado, obtém `tplId = item["templateid"]` — este é o **ID do item dentro do template**, não o ID do template em si.
2. Converte para o ID do template: `realTplId = cacheTemplateHostID[tplId]`. O cache `cacheTemplateHostID` foi preenchido anteriormente via `item.get` com `selectHosts`, mapeando o `templateid` do item para o `hostid` do template (que é o ID canônico do template no Zabbix).
3. Incrementa `templateCounter[realTplId]` — garante que todos os itens do mesmo template sejam agrupados corretamente, mesmo que possuam `templateid` diferentes entre si.
4. `topTemplates = sort(templateCounter) desc`
5. Nomes de templates resolvidos via `template.get` com `templateids: [...]` (única chamada em batch)

> **Por que o `cacheTemplateHostID` é necessário?** A API Zabbix retorna em `item["templateid"]` o ID do **item herdado dentro do template**, não o ID do template pai. Múltiplos itens do mesmo template têm `templateid` diferentes — sem a conversão, o mesmo template apareceria diversas vezes no ranking. O mapeamento correto é: `cacheTemplateHostID[templateid_do_item] → hostid_do_template`.

### Construção dos links de edição

| Versão | Formato do link |
|--------|----------------|
| Zabbix ≥ 7 | `zabbix.php?action=item.list&context=host&filter_hostids[]=<hostid>&filter_name=<item>` |
| Zabbix 6 | `items.php?form=update&hostid=<hostid>&itemid=<itemid>&context=host` |

---

## Guia 6: Top Hosts/Templates/Itens (`tab-top`)

### O que é

Exibe quatro rankings baseados nos itens não suportados coletados:

1. **Top Templates Ofensores** — templates com mais itens problemáticos
2. **Top Hosts Ofensores** — hosts com mais itens problemáticos (mostra o template mais recorrente por host)
3. **Top Itens Problemáticos** — chaves de item com maior número de erros
4. **Tipos de Erro Mais Comuns** — mensagens de erro mais frequentes

### Tabelas exibidas

**Top Templates Ofensores:**

| Template | Quantidade de Erros |
|----------|-------------------|

**Top Hosts Ofensores:**

| Host | Template Mais Ofensor | Quantidade de Erros |
|------|----------------------|-------------------|

**Top Itens Problemáticos:**

| Item | Template | Quantidade de Erros |
|------|----------|-------------------|

**Tipos de Erro Mais Comuns:**

| Mensagem de Erro | Template | Ocorrências |
|------------------|----------|-------------|

### Chamadas à API do Zabbix

**Nenhuma chamada nova.** Todos os dados vêm dos agrupamentos feitos na fase inicial sobre o `item.get` com `state:1`. Os contadores são:

- `templateCounter[realTplId]` — erros por template (usando ID canônico do template via `cacheTemplateHostID`)
- `hostCounter[hostname]` — erros por host
- `itemCounter[itemname|realTplId]` — erros por item dentro do template
- `errorCounter[errormsg|realTplId]` — erros por mensagem dentro do template

O `realTplId` é derivado de `cacheTemplateHostID[item["templateid"]]`, garantindo que todos os itens do mesmo template sejam agrupados sob uma única chave. Sem essa conversão, o mesmo template poderia aparecer múltiplas vezes nos rankings com contagens separadas.

O Top N é 10 por padrão (constante `topN = 10`).

---

## Guia 7: Recomendações (`tab-recomendacoes`)

### O que é

Sugestões automáticas geradas com base em todos os dados coletados. Organizada em 5 seções numeradas com KPI cards no topo para visão rápida:

1. **Zabbix Server** — processos em Atenção + sugestão de pollers assíncronos (Zabbix 7)
2. **Zabbix Proxys** — lista proxies Unknown/Offline com orientações
3. **Items** — itens sem template, não suportados, desabilitados, intervalo curto, texto com histórico, SNMP get/walk
4. **Regras de LLD** — LLD com intervalo curto e LLD não suportadas
5. **Templates** — lista dos top templates para revisão e erros mais comuns

### KPI cards

Os KPIs são exibidos em uma faixa horizontal no topo da guia Recomendações. Cada card é **clicável** e rola a página até a seção correspondente. A ordem de exibição é sempre a mesma:

| # | Label | Cor | Variável Go | Clique leva para | Condição de exibição |
|---|-------|-----|-------------|-----------------|----------------------|
| 1 | Zabbix Server - Process/Pollers com AVG alto | 🟡 Amarelo (`kpi-warn`) | `attentionCount` = `len(attention)` | Seção Zabbix Server | Sempre |
| 2 | Proxys Offline | 🔴 Vermelho (`kpi-crit`) | `proxyOfflineCount` | Seção Zabbix Proxys | Sempre |
| 3 | Proxys Unknown | ⚪ Neutro | `proxyUnknownCount` | Seção Zabbix Proxys | Sempre |
| 4 | Proxys - Process/Pollers com AVG alto | 🟢 Verde / 🟡 Amarelo | `len(proxyProcAttnList)` | Seção Zabbix Proxys | Sempre |
| 5 | Items Não Suportados | 🔴 Vermelho (`kpi-crit`) | `unsupportedCount` | Seção Items | Sempre |
| 6 | Templates SNMP p/ Poller Assíncrono | 🟢 Verde / 🟡 Amarelo | `len(snmpMigrationTpls)` | Seção Templates | **Zabbix ≥ 7 apenas** |
| 7 | Items - SNMP-POLLER | 🟢 Verde / 🔴 Vermelho | `snmpPct` (%) | Seção Items | **Zabbix ≥ 7 apenas** |
| 8 | Items Texto c/ Histórico | 🟡 Amarelo (`kpi-warn`) | `textItemsCount` | Seção Items | Sempre |

### Lógica de cor dos KPIs

| KPI | Condição | Classe CSS | Borda |
|-----|----------|------------|-------|
| Zabbix Server - Process/Pollers | sempre amarelo | `kpi-warn` | amarelo `#ffcc00` |
| Proxys Offline | sempre vermelho | `kpi-crit` | vermelho `#ff6666` |
| Proxys Unknown | sempre neutro | _(sem classe)_ | cinza |
| Proxys - Process/Pollers | `len(proxyProcAttnList) == 0` → verde; `> 0` → amarelo | `kpi-ok` / `kpi-warn` | verde / amarelo |
| Items Não Suportados | sempre vermelho | `kpi-crit` | vermelho `#ff6666` |
| Templates SNMP p/ Poller Assíncrono | `len(snmpMigrationTpls) == 0` → verde; `> 0` → amarelo | `kpi-ok` / `kpi-warn` | verde / amarelo |
| Items - SNMP-POLLER (%) | `snmpPct >= 80%` → verde; `< 80%` → vermelho | `kpi-ok` / `kpi-crit` | verde / vermelho |
| Items Texto c/ Histórico | sempre amarelo | `kpi-warn` | amarelo `#ffcc00` |

> **Referência de limiar:** processos do **Zabbix Server** em atenção usam avg ≥ 50% (variável `attention`); processos dos **Proxys** usam avg ≥ 60% (variável `proxyProcAttnList`).

### Chamadas à API do Zabbix (exclusivas desta guia)

Apenas para Zabbix ≥ 7, dois `item.get` para os KPIs de SNMP (compartilhados com o item "3.x) Items SNMP-POLLER" na Seção Items e com a subseção "Templates passíveis para migração para SNMP-POLLER"):

| Chamada | Parâmetros relevantes | Variável Go preenchida |
|---------|-----------------------|------------------------|
| `item.get` | `filter:{type:20}, templated:true, countOutput:true` | `snmpTplCount` |
| `item.get` | `filter:{type:20}, search:{snmp_oid:["get[*","walk[*"]}, searchWildcardsEnabled:true, searchByAny:true, countOutput:true, templated:true` | `snmpGetWalkCount` |
| `item.get` ×2 + `template.get` | _(ver subseção "Templates passíveis para migração")_ | `snmpMigrationTpls` |

### Funções auxiliares usadas

| Função | Descrição |
|--------|-----------|
| `pct(part, total int) string` | Formata percentual `"0.00%"`; retorna `"0%"` se total=0 |
| `titleWithInfo(tag, title, tip string) string` | Gera heading HTML com ícone `?` e tooltip |

### Como adicionar um novo KPI

1. **Calcule o dado** antes do bloco `html += "<div class='rec-kpis'>"` em `cmd/app/main.go`.
2. **Defina a classe CSS** (`kpi-ok`, `kpi-warn`, `kpi-crit` ou vazio para neutro) com base no valor.
3. **Insira o `<div>`** na posição desejada dentro do bloco `rec-kpis`, seguindo o padrão:
```go
html += `<div class='kpi ` + classe + `' data-target='#card-<seção>' title='<tooltip>'>`+
    `<div class='kpi-num'>` + valor + `</div>`+
    `<div class='kpi-label'>Label do KPI</div></div>`
```
4. O clique para rolar até a seção é gerenciado automaticamente pelo JavaScript inline logo após o bloco `rec-kpis` — **nenhuma alteração de JS é necessária**.

---

### Item "Items SNMP-POLLER (Zabbix 7)" — Seção 3 (Items)

#### O que é

Aparece na **Seção 3 — Items** das Recomendações (subitem `3.x)`), **somente** para Zabbix ≥ 7 e quando há pelo menos 1 item SNMP em templates (`snmpTplCount > 0`). Exibe:

- Total de items SNMP em templates (`snmpTplCount`)
- Quantidade já usando OID `get[]`/`walk[]` — formato do poller assíncrono (`snmpGetWalkCount`)
- Percentual migrado em relação ao total de items do ambiente

Aponta o colaborador para a subseção **"Templates passíveis para migração para SNMP-POLLER"** para ver os templates específicos que precisam de atenção.

#### Condição de exibição

```
majorV >= 7  AND  snmpTplCount > 0
```

#### Chamadas à API

Essas 2 chamadas são **compartilhadas** com o KPI card "Items - SNMP-POLLER" no topo das Recomendações — os valores são coletados uma única vez e reutilizados:

| Chamada | Parâmetros relevantes | Variável Go |
|---------|-----------------------|-------------|
| `item.get` | `filter:{type:20}, templated:true, countOutput:true` | `snmpTplCount` |
| `item.get` | `filter:{type:20}, search:{snmp_oid:["get[*","walk[*"]}, searchWildcardsEnabled:true, searchByAny:true, countOutput:true, templated:true` | `snmpGetWalkCount` |

> **`type:20`** corresponde ao tipo SNMP no Zabbix. Os OIDs `get[*` e `walk[*` são o padrão do Zabbix 7 para o poller assíncrono; OIDs no formato antigo (ex.: `.1.3.6.1.2.1.1.1.0`) **não** casam com esse filtro.

#### Lógica de exibição no HTML

```go
if majorV >= 7 && snmpTplCount > 0 {
    // exibe item 3.x com snmpTplCount, snmpGetWalkCount e pct(snmpGetWalkCount, totalItemsVal)
    // aponta para a seção "Templates passíveis para migração para SNMP-POLLER"
}
```

---

### Subseção "Templates passíveis para migração para SNMP-POLLER" — Seção 5 (Templates)

#### O que é

Aparece ao final da **Seção 5 — Templates** das Recomendações, **somente** para Zabbix ≥ 7 e quando há pelo menos 1 template legado em uso (`len(snmpMigrationTpls) > 0`). Lista alfabeticamente os nomes dos templates que satisfazem **todas** estas condições:

1. Possuem ao menos um item SNMP (`type=20`) em templates
2. **Ainda não** utilizam OID no formato `get[]` ou `walk[]` (poller assíncrono do Zabbix 7)
3. Estão **vinculados a pelo menos um host** (realmente em uso no ambiente)

Templates totalmente migrados (onde **todos** os items SNMP já usam `get[]`/`walk[]`) são excluídos automaticamente.

> **Nota:** Um template com **mistura** de OIDs antigos e novos ainda aparece na lista. Somente quando **100% dos items SNMP** usam o formato moderno o template é removido. Isso garante que migrações incompletas também sejam revisadas.

#### Chamadas à API do Zabbix (exclusivas desta subseção)

São **3 chamadas** feitas sequencialmente após os `countOutput` do KPI, **somente** quando `majorV >= 7`:

##### Chamada 1 — Todos os items SNMP em templates (com hostid do template)

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid"],
    "filter": { "type": 20 },
    "templated": true,
    "selectHosts": ["hostid"]
  }
}
```

- `selectHosts: ["hostid"]` retorna, em cada item, o campo `hosts` com o `hostid` do template pai.
- Usado para construir o conjunto de **todos os hostids de templates SNMP** (`allSnmpHostids`).

##### Chamada 2 — Items SNMP que já usam get[]/walk[] (hostid do template)

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid"],
    "filter": { "type": 20 },
    "search": { "snmp_oid": ["get[*", "walk[*"] },
    "searchWildcardsEnabled": true,
    "searchByAny": true,
    "templated": true,
    "selectHosts": ["hostid"]
  }
}
```

- `search` + `searchWildcardsEnabled` filtra apenas OIDs que **começam** com `get[` ou `walk[`.
- Resultado → conjunto `modernHostids` (hostids dos templates que têm ao menos 1 item moderno).
- A diferença `allSnmpHostids − modernHostids` = `legacyHostSet` (templates com apenas OIDs legados).

##### Chamada 3 — Resolução dos nomes dos templates legados

```json
{
  "method": "template.get",
  "params": {
    "output": ["templateid", "name"],
    "templateids": ["<ids do legacyHostSet>"],
    "selectHosts": ["hostid"]
  }
}
```

- `selectHosts: ["hostid"]` retorna o campo `hosts` por template.
- Templates com `hosts == []` (sem vínculo com nenhum host) são **descartados** — não aparecem na lista.
- Os nomes dos templates restantes são ordenados alfabeticamente e armazenados em `snmpMigrationTpls`.

#### Fluxo completo de construção

```
Chamada 1 → allSnmpHostids   (todos os hostids de templates com items SNMP)
Chamada 2 → modernHostids    (hostids de templates com ao menos 1 OID get[]/walk[])

legacyHostSet = allSnmpHostids − modernHostids

Chamada 3 → template.get(legacyHostSet)
              → descarta len(hosts) == 0   (não vinculado a host)
              → sort.Strings()
              → snmpMigrationTpls
```

#### Variável Go resultante

| Variável | Tipo | Conteúdo |
|----------|------|----------|
| `snmpMigrationTpls` | `[]string` | Lista ordenada A-Z de nomes de templates legados **em uso** |

#### Condição de exibição no HTML

```go
if majorV >= 7 && len(snmpMigrationTpls) > 0 {
    // renderiza h5 com titleWithInfo + <ul> com os nomes dos templates
}
```

Se `snmpMigrationTpls` estiver vazio (todos os templates já migrados ou sem ambiente Zabbix 7), a subseção **não aparece** no relatório.

#### Como interpretar a lista

| Situação | Significado |
|----------|-------------|
| Subseção não exibida | Todos os templates SNMP em uso já usam `get[]`/`walk[]` — parabéns! |
| Template aparece na lista | Tem ao menos 1 item SNMP com OID legado **e** está vinculado a ao menos 1 host |
| KPI verde (≥ 80%) + lista presente | Maioria migrou, mas ainda há templates específicos com resquícios de OIDs antigos |
| KPI vermelho (< 80%) + lista longa | Ambiente com grande parte dos templates SNMP ainda usando poller síncrono |

#### Como adicionar/remover lógica de detecção

O bloco completo está em `cmd/app/main.go`, logo após os dois `item.get countOutput` do KPI SNMP, dentro do `if majorV >= 7 { ... }`. Para alterar o critério de "legado" (ex.: incluir outro tipo de OID antigo), ajuste o `search.snmp_oid` da **Chamada 2**:

```go
// Chamada 2 — adicionar novo padrão de OID moderno:
"search": map[string]interface{}{
    "snmp_oid": []string{"get[*", "walk[*", "novo_formato[*"},
},
```

---

## Relatórios Salvos (Persistência no Banco de Dados)

### Visão geral

Toda vez que um relatório é gerado com sucesso, ele é **salvo automaticamente** no PostgreSQL — sem nenhuma ação extra do usuário. Isso permite acessar relatórios anteriores a qualquer momento, mesmo após reiniciar a aplicação.

> **O banco de dados é opcional.** O app funciona normalmente sem PostgreSQL — os relatórios ficam disponíveis apenas na sessão atual (in-memory) e se perdem ao reiniciar o container.
>
> Para habilitar a persistência, defina `DB_HOST` (e opcionalmente as demais variáveis `DB_*`) e suba o serviço `postgres` com o profile `db` (veja detalhes em [docker.md](docker.md)).

Quando `DB_HOST` **não** está configurada:
- O card **Relatórios Salvos** é **ocultado automaticamente** na interface web
- Os endpoints `/api/reports`, `/api/reportdb/:id` e `DELETE /api/reports` retornam `db_enabled: false` ou `404`
- Nenhum erro é exibido ao usuário — o app simplesmente opera sem banco

---

### Como usar a interface de Relatórios Salvos

Na tela inicial, abaixo do botão **Gerar Relatório**, existe o card **Relatórios Salvos** com os seguintes controles:

```
┌─ RELATÓRIOS SALVOS ──────────────────────── [↺ Carregar lista] ─┐
│                                                                   │
│  [── selecione um relatório ──────────────]  [Abrir] [Excluir]   │
│                                              [Excluir Todos]      │
└───────────────────────────────────────────────────────────────────┘
```

#### Passo a passo

| Passo | Ação | O que acontece |
|-------|------|----------------|
| 1 | Clique em **↺ Carregar lista** | A lista dos últimos 20 relatórios salvos é carregada no seletor. Cada entrada mostra a URL do Zabbix e a data/hora da geração. |
| 2 | Selecione um relatório no menu suspenso | O relatório fica destacado — nenhuma ação ainda. |
| 3 | Clique em **Abrir** | O relatório selecionado é carregado inline, com todas as guias, botões de exportar HTML e imprimir PDF funcionando normalmente. |
| 4 _(opcional)_ | Clique em **Excluir** | Remove **apenas o relatório selecionado** do banco após confirmação. A lista é atualizada automaticamente. |
| 5 _(opcional)_ | Clique em **Excluir Todos** | Remove **todos os relatórios** do banco após confirmação dupla. Retorna a contagem de registros removidos. |

> **Dica:** Após abrir um relatório salvo, todos os botões de **Exportar HTML** e **Gerar PDF** funcionam normalmente — o relatório reabre com o mesmo layout e funcionalidades da geração original.

---

### Quando os relatórios são salvos

O salvamento acontece automaticamente ao final da função `generateZabbixReportWithProgress`, no fluxo:

```
POST /api/start
  → goroutine: generateZabbixReport()
      → relatório HTML gerado com sucesso
      → INSERT INTO reports (name, format, content, zabbix_url) VALUES (...)
         name     = "report-<unix timestamp>"
         format   = "html"
         content  = HTML completo (BYTEA)
         zabbix_url = URL informada no formulário
```

Se o banco não estiver disponível, o relatório ainda é exibido na tela normalmente — o erro de persistência é apenas logado (`[WARN] Failed to save report to DB`).

---

### Variáveis de ambiente para o banco

| Variável    | Padrão     | Descrição |
|-------------|------------|-----------|
| `DB_HOST`   | _(vazio)_  | Host do PostgreSQL. **Se não definida, a persistência é desativada.** |
| `DB_PORT`   | `5432`     | Porta do PostgreSQL. |
| `DB_USER`   | `postgres` | Usuário do banco. |
| `DB_PASSWORD` | `postgres` | Senha do banco. |
| `DB_NAME`   | `zabbix_report` | Nome do banco de dados. |

Exemplo no `docker-compose.yml` (as variáveis ficam **comentadas por padrão**; descomente para ativar):
```yaml
environment:
  # Descomente para habilitar persistência de relatórios no PostgreSQL:
  #- DB_HOST=postgres
  #- DB_PORT=5432
  #- DB_USER=postgres
  #- DB_PASSWORD=postgres
  #- DB_NAME=zabbix_report
```

> **Como subir com banco:** `docker compose --profile db up --build -d`
> O profile `db` é necessário porque o serviço `postgres` só é iniciado quando explicitamente solicitado.
> O `go-app` **não** depende do postgres no `depends_on` — se o banco não estiver disponível, o app sobe normalmente sem persistência.

---

### Schema do banco de dados

A tabela é criada automaticamente na primeira execução se não existir:

```sql
CREATE TABLE reports (
  id         SERIAL PRIMARY KEY,
  name       TEXT,            -- "report-<timestamp>" gerado automaticamente
  format     TEXT,            -- sempre "html" por enquanto
  content    BYTEA,           -- HTML completo do relatório
  zabbix_url TEXT,            -- URL do Zabbix usada na geração
  created_at TIMESTAMPTZ DEFAULT now()
);
```

> **Migração automática:** Se a tabela existir com colunas faltando (schema antigo), a aplicação renomeia a tabela antiga para `reports_old_<timestamp>` e cria uma nova com o schema correto.

---

### API REST — Endpoints de relatórios

Para integrações externas ou automações, os endpoints disponíveis são:

| Endpoint | Método | Banco necessário | Descrição |
|----------|--------|------------------|-----------|
| `POST /api/start` | POST | Não | Inicia geração; corpo `{"zabbix_url":"...","zabbix_token":"..."}` → retorna `{"task_id":"..."}` |
| `GET /api/progress/:id` | GET | Não | Polling do status da tarefa → `{"status":"done\|running\|error","progress_msg":"..."}` |
| `GET /api/report/:id` | GET | Não | Retorna o HTML do relatório da sessão atual (in-memory, perde ao reiniciar) |
| `GET /api/db-status` | GET | Não | Informa ao frontend se o banco está ativo → `{"db_enabled": true\|false}` |
| `GET /api/reports` | GET | Opcional | Sem banco: `{"db_enabled":false,"reports":[]}`. Com banco: `{"db_enabled":true,"reports":[...]}` |
| `GET /api/reportdb/:id` | GET | **Sim** | Retorna o relatório salvo como documento HTML completo (para abrir no browser diretamente) |
| `GET /api/reportdb/:id?raw=1` | GET | **Sim** | Retorna apenas o fragment HTML interno (usado pelo JS para renderizar inline com o layout padrão) |
| `DELETE /api/reportdb/:id` | DELETE | **Sim** | Remove um relatório específico pelo ID → `{"deleted":"<id>"}` ou `404` se não encontrado |
| `DELETE /api/reports` | DELETE | **Sim** | Remove **todos** os relatórios → `{"deleted":<contagem>}` |

#### Exemplo: listar e abrir um relatório via curl

```bash
# 1. Listar relatórios salvos
curl http://localhost:8080/api/reports

# 2. Abrir o relatório com ID 3 no browser
curl http://localhost:8080/api/reportdb/3 > relatorio.html

# 3. Excluir o relatório com ID 3
curl -X DELETE http://localhost:8080/api/reportdb/3

# 4. Excluir todos os relatórios
curl -X DELETE http://localhost:8080/api/reports
```

O relatório é dividido em guias (tabs). Esta seção documenta cada uma delas: o que exibe, quais chamadas à API do Zabbix são feitas, qual função Go gera o HTML e como os dados são tratados.

---

## Guia: Zabbix Server (`tab-processos`)

### O que é

Exibe o nível de utilização dos processos internos do Zabbix Server, divididos em dois grupos:

- **Pollers (Data Collectors):** processos responsáveis por coletar métricas ativamente dos agentes (poller, http poller, icmp pinger, agent poller, snmp poller, etc.)
- **Internal Processes:** processos de infraestrutura do servidor (history syncer, housekeeper, escalator, trapper, lld manager, etc.)

Para cada processo é exibido o **mínimo**, **média** e **máximo** de utilização (%), além de um status visual **OK** (< 50%) ou **Atenção** (≥ 50%).

### Tabela exibida

| Coluna       | Descrição                                                  |
|--------------|------------------------------------------------------------|
| Poller / Processo | Nome do processo com ícone `?` de tooltip explicativo |
| value_min    | Mínimo de utilização no período analisado                  |
| value_avg    | Média de utilização no período analisado                   |
| value_max    | Pico de utilização no período analisado                    |
| Status       | `OK` (verde, avg < 50%) ou `Atenção` (vermelho, avg ≥ 50%) ou cinza quando não habilitado |

### Variáveis de ambiente que influenciam esta guia

| Variável              | Padrão | Descrição                                                                                   |
|-----------------------|--------|---------------------------------------------------------------------------------------------|
| `ZABBIX_SERVER_HOSTID`| _(vazio)_ | ID do host do Zabbix Server no Zabbix. Se não definida, a busca ignora o filtro por host e pode retornar qualquer host que tenha a chave. Recomendado definir para garantir precisão. |
| `CHECKTRENDTIME`      | `30d`  | Janela de tempo para análise dos trends/histórico. Aceita sufixo `d` (dias), `h` (horas), `m` (minutos). Ex: `7d`, `24h`. |
| `MAX_CCONCURRENT`     | `6`    | Numero de processos que podem ser executados em concorrência nas chamadas paralelas à API do Zabbix.                            |

### Chamadas à API do Zabbix

Cada processo da lista consulta o **mapa pré-carregado** por `getProcessItemsBulk` (1 chamada feita antes das goroutines) e depois faz **duas chamadas** paralelas para obter os dados de trend/history:

#### 1. `item.get` bulk — localizar todos os itens de processos (1 chamada única)

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid", "name", "key_", "value_type"],
    "search": { "key_": ["*agent*poller*", "*poller*", "*history*syncer*", "..."] },
    "searchByAny": true,
    "searchWildcardsEnabled": true,
    "hostids": "<ZABBIX_SERVER_HOSTID>"
  }
}
```

- A chave **não** é mais `zabbix[process,<nome>,avg,busy]` — é buscada por wildcard e casa com qualquer formato de chave (espaço ou underscore).
- Se `ZABBIX_SERVER_HOSTID` não estiver definida, o parâmetro `hostids` é omitido.
- Resultado vazio para um processo → marcado como **"Processo não habilitado"** (cinza).

#### 2a. `trend.get` — buscar estatísticas de trend (primária)

```json
{
  "method": "trend.get",
  "params": {
    "output": ["itemid", "clock", "value_min", "value_avg", "value_max"],
    "itemids": ["<itemid>"],
    "time_from": "<agora - CHECKTRENDTIME>",
    "time_to": "<agora>",
    "limit": 1
  }
}
```

- Retorna o último registro de trend no período configurado.
- Resultado `[]` → aciona o fallback abaixo.

#### 2b. `history.get` — fallback quando trend não está disponível

```json
{
  "method": "history.get",
  "params": {
    "output": ["value"],
    "history": 0,
    "itemids": ["<itemid>"],
    "time_from": "<agora - CHECKTRENDTIME>",
    "time_to": "<agora>",
    "sortfield": "clock",
    "sortorder": "ASC",
    "limit": 2000
  }
}
```

- Usado quando o item tem `trends=0` configurado no Zabbix, ou quando o período de retenção de trends já expirou.
- O código coleta até 2.000 pontos e calcula manualmente `min`, `avg` e `max`.
- Resultado ainda `[]` → processo marcado como **"Processo não habilitado"** (cinza).

### Função Go responsável

**Arquivo:** `cmd/app/main.go`  
**Função:** `generateZabbixReport(url, token string)` — o bloco da guia começa em torno da linha marcada com `// --- Processos e Threads Zabbix Server ---`

#### Helpers utilizados

| Função | Descrição |
|--------|-----------|
| `nameToWildcard(name)` | Converte `"agent poller"` → `"*agent*poller*"` para a busca wildcard |
| `wildcardMatch(pattern, key)` | Match client-side simples (`*`) para mapear items retornados de volta a cada nome |
| `getProcessItemsBulk(apiUrl, token, names, hostid)` | Faz **1 `item.get`** com todos os padrões. Resolve colisões por especificidade (mais palavras = prioridade). Retorna `map[nomeLowercase]item` |
| `getLastTrend(apiUrl, token, itemid, days)` | Faz `trend.get` para o itemid no período configurado. Respeita `CHECKTRENDTIME`. |
| `getHistoryStats(apiUrl, token, itemid, histType, days)` | Fallback: faz `history.get`, coleta até 2.000 pontos e retorna `{value_min, value_avg, value_max}` calculados. |

#### Lógica de versão

A lista de pollers varia conforme a versão do Zabbix detectada via `apiinfo.version`:

- **Zabbix ≥ 7:** inclui `agent poller`, `browser poller`, `http agent poller`, `snmp poller`
- **Zabbix 6:** esses quatro são exibidos como **"Não existe nesta versão do Zabbix"**

#### Lógica de status

| Condição                          | Exibição                                      |
|-----------------------------------|-----------------------------------------------|
| `item.get` retorna vazio          | Cinza — "Processo não habilitado"             |
| `trend.get` e `history.get` vazios| Cinza — "Processo não habilitado"             |
| `value_avg < 50%`                 | Verde — "OK"                                  |
| `value_avg ≥ 50%`                 | Vermelho — "Atenção"                          |
| Erro em qualquer chamada API      | "Erro ao obter dados"                         |

#### Paralelismo

Tanto os pollers quanto os internal processes são coletados em **goroutines paralelas**, controladas por um semáforo com capacidade `MAX_CCONCURRENT`. Os resultados são reordenados pelo índice original para manter a ordem de exibição, depois reordenados por `value_avg` decrescente (processos mais utilizados aparecem primeiro).

### Como adicionar um novo processo à lista

São **3 lugares** em `cmd/app/main.go`:

**1. `procDesc`** — descrição exibida no tooltip `?` (chave em lowercase):
```go
"novo processo": `Parâmetro "StartNovoProcesso": descrição e quando ajustar.`,
```

**2. Tabela correta:**

- **Pollers (Data Collectors)** → `commonPollers` ou bloco `if majorV >= 7`:
```go
commonPollers := []string{
    ...
    "novo poller",
}
// ou exclusivo do Zabbix 7+:
if majorV >= 7 {
    pollerNames = append([]string{"novo poller"}, pollerNames...)
}
```

- **Internal Process** → slice `procNames`:
```go
procNames := []string{
    ...
    "novo processo",
}
```

**3. Regra do nome:** use o nome exatamente como aparece na chave do item no Zabbix (com espaço ou underscore). A função `nameToWildcard` converte automaticamente — `"agent poller"` → `"*agent*poller*"` — casando com `agent poller`, `agent_poller` ou qualquer variante.

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
  - Busca **bulk de todos os itens de processo** (pollers + internal) em 1 chamada com wildcard
  - Buscar itens por chave (`key_`) e hostid
  - Contar itens totais, habilitados, desabilitados, não suportados
  - Listar itens não suportados e seus detalhes
  - Buscar itens sem template
  - Buscar itens por tipo (`type`), estado (`state`), intervalo (`delay`)
- **Exemplos de parâmetros:**
  - Busca bulk de processos do servidor (nova abordagem):
    ```json
    {
      "output": ["itemid", "hostid", "name", "key_", "value_type"],
      "search": { "key_": ["*agent*poller*", "*history*syncer*", "*housekeeper*", "..."] },
      "searchByAny": true,
      "searchWildcardsEnabled": true,
      "hostids": "<ZABBIX_SERVER_HOSTID>"
    }
    ```
  - Buscar item por chave exata:
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

### 9. proxy.get
- **Descrição:** Busca proxies cadastrados, por estado (ativo/passivo), status (online/offline), etc.
- **Parâmetros:**
  - Todos os proxies: `{ "output": "proxyid" }`
  - Ativos: `{ "output": "proxyid", "filter": { "state": 2 } }`
  - Passivos: `{ "output": "proxyid", "filter": { "state": 1 } }`
  - Online: `{ "output": "proxyid", "filter": { "status": 0 } }`
  - Offline: `{ "output": "proxyid", "filter": { "status": 1 } }`

## Top Erros
  Verifica os tops erros de itens, triggers e LLD rules, ordenados por número de falhas.

  Chamada API usada para coletar os itens com erro (itens "não suportados"):

  Parâmetros: item.get com
  output: ["itemid","name","templateid","error","key_"]
  filter: { "state": 1 }
  webitems: 1
  selectHosts: ["name","hostid"]
  inherited: true
  Como errorCounter é preenchido:

  O código itera cada item retornado, extrai templateid, name, error e host.
  Incrementa o mapa errorCounter com a chave formada por itemError + "|" + tplId, onde itemError é o texto do erro e tplId é o ID do template (ou "no_template" se não houver).
  topErrors é percorrido para gerar a tabela de "Tipos de Erro Mais Comuns".

---

Essas chamadas são feitas dinamicamente conforme a versão do Zabbix e os dados do ambiente. Consulte o código para detalhes de parâmetros opcionais e lógica de fallback.

