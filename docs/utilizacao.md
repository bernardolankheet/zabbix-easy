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

### Chamadas à API do Zabbix (por processo, em paralelo)

#### 1. `item.get` — localizar o item

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid", "name", "key_", "value_type"],
    "filter": { "key_": "zabbix[process,<nome>,avg,busy]" },
    "hostids": "<ZABBIX_SERVER_HOSTID>",
    "limit": 1
  }
}
```

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
| `getItemByKey(apiUrl, token, key, hostid)` | `item.get` com filtro exato na `key_`; com cache (`sync.Map`) |
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

```go
// Para Pollers:
pollerNames = append(pollerNames, "novo poller")

// Para Internal Processes:
procNames = append(procNames, "novo processo")

// Descrição (map procDesc):
procDesc["novo processo"] = `Parâmetro "XYZ": descrição e quando ajustar.`
```

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

| KPI | Cor | Dado |
|-----|-----|------|
| Process/Pollers com AVG alto | Amarelo | `len(attention)` — processos com avg ≥ 50% |
| Proxys Offline | Vermelho | `offline` |
| Proxys Unknown | Neutro | `unknown` |
| Itens Não Suportados | Vermelho | `unsupportedVal` |
| Items SNMP-POLLER (%) | Verde/Vermelho | `snmpGetWalkCount / snmpTplCount * 100` (só Zabbix 7) |
| Itens Texto c/ Histórico | Amarelo | `textCount` |

### Chamadas à API do Zabbix (exclusivas desta guia)

Apenas para Zabbix ≥ 7, dois `item.get` para o KPI de SNMP:

| Chamada | Parâmetros relevantes | Dado extraído |
|---------|-----------------------|---------------|
| `item.get` | `filter:{type:20}, templated:true, countOutput:true` | Total de items SNMP em templates |
| `item.get` | `filter:{type:20}, search:{snmp_oid:["get[*","walk[*"]}, searchWildcardsEnabled:true, searchByAny:true, countOutput:true, templated:true` | Items SNMP usando OID get[] ou walk[] (poller assíncrono) |

### Lógica do KPI SNMP

```
snmpPct = snmpGetWalkCount / snmpTplCount * 100
≥ 80% → kpi-ok (verde)   — maioria já usa poller assíncrono
< 80% → kpi-crit (vermelho) — muitos items SNMP ainda usam OID antigo
```

### Funções auxiliares usadas

| Função | Descrição |
|--------|-----------|
| `pct(part, total int) string` | Formata percentual `"0.00%"`; retorna `"0%"` se total=0 |
| `titleWithInfo(tag, title, tip string) string` | Gera heading HTML com ícone `?` e tooltip |

---

## Persistência e API REST

| Endpoint | Método | Descrição |
|----------|--------|-----------|
| `POST /api/start` | POST | Inicia geração; recebe `{zabbix_url, zabbix_token}`; retorna `{task_id}` |
| `GET /api/progress/:id` | GET | Polling de status; retorna `{status, progress_msg, report}` |
| `GET /api/report/:id` | GET | Retorna HTML do relatório gerado na sessão atual (in-memory) |
| `GET /api/reports` | GET | Lista relatórios salvos no PostgreSQL (últimos 20) |
| `GET /api/reportdb/:id` | GET | Retorna relatório salvo no banco como documento completo |
| `GET /api/reportdb/:id?raw=1` | GET | Retorna apenas o fragment HTML para renderização inline pelo JS |
| `DELETE /api/reportdb/:id` | DELETE | Remove relatório do banco |

### Schema PostgreSQL

```sql
CREATE TABLE reports (
  id         SERIAL PRIMARY KEY,
  name       TEXT,
  format     TEXT,
  content    BYTEA,
  zabbix_url TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);
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

Cada processo da lista faz **duas chamadas** (executadas em paralelo via goroutines com semáforo):

#### 1. `item.get` — localizar o item do processo

```json
{
  "method": "item.get",
  "params": {
    "output": ["itemid", "hostid", "name", "key_", "value_type"],
    "filter": { "key_": "zabbix[process,<nome>,avg,busy]" },
    "hostids": "<ZABBIX_SERVER_HOSTID>",
    "limit": 1
  }
}
```

- A chave é construída dinamicamente: `zabbix[process,poller,avg,busy]`, `zabbix[process,history syncer,avg,busy]`, etc.
- Se `ZABBIX_SERVER_HOSTID` não estiver definida, o parâmetro `hostids` é omitido.
- Resultado `[]` → processo marcado como **"Processo não habilitado"** (cinza).

#### 2a. `trend.get` — buscar estatísticas de trend (primária)

```json
{
  "method": "trend.get",
  "params": {
    "output": ["itemid", "clock", "value_min", "value_avg", "value_max"],
    "itemids": ["<itemid>"],
    "time_from": <agora - CHECKTRENDTIME>,
    "time_to": <agora>,
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
    "time_from": <agora - CHECKTRENDTIME>,
    "time_to": <agora>,
    "sortfield": "clock",
    "sortorder": "ASC",
    "limit": 2000
  }
}
```

- Usado quando o item tem `trends=0` configurado no Zabbix, ou quando o período de retenção de trends já expirou.
- O `history` type é `0` (float) por padrão; se o `value_type` do item for `3` (inteiro), usa `3`.
- O código coleta até 2.000 pontos e calcula manualmente `min`, `avg` e `max`.
- Resultado ainda `[]` → processo marcado como **"Processo não habilitado"** (cinza).

### Função Go responsável

**Arquivo:** `cmd/app/main.go`  
**Função:** `generateZabbixReport(url, token string)` — o bloco da guia começa em torno da linha marcada com `// --- Processos e Threads Zabbix Server ---`

#### Helpers utilizados

| Função                      | Descrição                                                                                  |
|-----------------------------|--------------------------------------------------------------------------------------------|
| `getItemByKey(apiUrl, token, key, hostid)` | Faz `item.get` com filtro exato na `key_`. Tem cache em memória (`sync.Map`) para evitar chamadas duplicadas durante a geração. |
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

Para incluir um novo tipo de processo na tabela de **Pollers**, edite o slice `pollerNames` (ou o bloco `if majorV >= 7`) em `generateZabbixReport`. Para **Internal Processes**, edite o slice `procNames`. Em seguida, adicione a descrição correspondente no map `procDesc`.

```go
// Exemplo: adicionar "novo poller" para Zabbix 7+
if majorV >= 7 {
    pollerNames = append([]string{"novo poller"}, pollerNames...)
}

// Adicionar descrição
procDesc["novo poller"] = `Parâmetro "StartNovoPoller": descrição do parâmetro e quando ajustar.`
```

Nenhuma alteração de SQL ou frontend é necessária.
