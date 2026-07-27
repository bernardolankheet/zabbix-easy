# Zabbix Easy Report — Histórico de Atualizações e Implementações

Este documento descreve a evolução do **Zabbix Easy Report** (`zabbix-eay-visual`) desde a base original do projeto até as melhorias implementadas na branch de desenvolvimento local, incluindo correções de bugs, nova aba de análise por host, refatoração de arquitetura e endurecimento operacional.

> Para instruções de instalação e uso, consulte [README.md](README.md) (EN) ou [README_pt_br.md](README_pt_br.md) (PT).  
> Para releases oficiais versionadas, consulte [CHANGELOG.md](CHANGELOG.md).

---

## Índice

1. [Sistema base (ponto de partida)](#1-sistema-base-ponto-de-partida)
2. [Evolução oficial do repositório (CHANGELOG)](#2-evolução-oficial-do-repositório-changelog)
3. [Melhorias implementadas nesta base local](#3-melhorias-implementadas-nesta-base-local)
4. [Arquitetura atual](#4-arquitetura-atual)
5. [Funcionalidades por área](#5-funcionalidades-por-área)
6. [Variáveis de ambiente](#6-variáveis-de-ambiente)
7. [API HTTP](#7-api-http)
8. [Testes e CI](#8-testes-e-ci)
9. [Como validar](#9-como-validar)
10. [Próximos passos sugeridos](#10-próximos-passos-sugeridos)

---

## 1. Sistema base (ponto de partida)

O **Zabbix Easy Report** nasce como ferramenta open-source que:

- Conecta na **API JSON-RPC do Zabbix** (6.x / 7.x / 8.x experimental)
- Coleta dados de saúde do ambiente (hosts, items, templates, triggers, proxys, processos, alertas)
- Gera um **relatório HTML HealthCheck** com abas, KPIs, gauges e recomendações acionáveis
- Permite **exportação HTML/PDF** e persistência opcional em **PostgreSQL**

### Stack original

| Camada | Tecnologia |
|--------|------------|
| Backend | Go + Gin |
| Frontend | HTML/CSS/JS gerado pelo servidor |
| Coleta | Chamadas JSON-RPC centralizadas em `main.go` |
| Deploy | Docker, Helm (K8s), MkDocs para documentação |
| i18n | `pt_BR` e `en_US` via `messages.json` |

### Estrutura original simplificada

```
app/
├── cmd/app/main.go          # Monolito (~5000 linhas): API, coleta, HTML, handlers
├── internal/collector/      # Coletores testáveis (introduzidos gradualmente)
└── web/
    ├── templates/index.html
    ├── static/script.js, style.css, custom.css
    └── locales/pt_BR|en_US/messages.json
```

### Limitações conhecidas da base

- Quase toda a lógica concentrada em `main.go`
- Filtro por host inexistente ou incompleto (sem histórico de alertas confiável)
- Gráficos Chart.js sem tratamento de abas ocultas
- Docker com binário glibc copiado manualmente (quebrava no Alpine)
- Sem proteção de API, sem resolução de host ambíguo, sem aba dedicada por host

---

## 2. Evolução oficial do repositório (CHANGELOG)

Resumo das releases documentadas em [CHANGELOG.md](CHANGELOG.md):

### v0.1.1 (2026-04-23)
- Coletor `CollectMediaTypes`
- Nova aba **Alertas** com falhas agrupadas por media type
- Melhoria na tabela de Items (status Enabled/Disabled)

### v0.1.0 (2026-04-05) — Primeiro release oficial
- Relatório HealthCheck completo (Server, Proxys, Items, Templates, Triggers, Users)
- Compatibilidade Zabbix 6.0–8.0
- PostgreSQL, Docker, Helm, documentação MkDocs PT/EN

### v0.0.5 (2026-03-28)
- Pacote `internal/collector` com helpers tipados
- Migração parcial de parsing JSON-RPC fora de `main.go`
- Testes unitários dos coletores

### v0.0.4 (2026-03-24/25)
- Autenticação **Bearer token** para Zabbix ≥ 7.2
- Aba **Triggers** agregada por template
- Links de navegação rápida nas recomendações

### v0.0.3 / v0.0.2 / v0.0.1
- Documentação MkDocs i18n
- Aba **Users** com teste de senha padrão Admin/zabbix
- Recomendações por proxy com snippets dinâmicos
- Accordion de recomendações colapsável

---

## 3. Melhorias implementadas nesta base local

Esta seção documenta as implementações realizadas na evolução do fork/workspace **`zabbix-eay-visual`**, organizadas por fase.

---

### Fase A — Correções críticas de coleta por host

#### Problema
O filtro por host não funcionava corretamente: histórico de alertas vazio, métricas erradas ou ausentes.

#### Correções

| Arquivo | O que foi feito |
|---------|-----------------|
| `collect_host_alert_history.go` | Reescrito: usa `event.get` com `hostids` em vez de `CollectTriggers(-1)` |
| `collect_host_detailed_report.go` | Fix Zabbix 7: `selectRelatedObject: "extend"` |
| `collect_items.go` | Novo `CollectItemsByHost()` — items filtrados por host |
| `collector.go` | Helper `ResolveHostByFilter()` |
| `collect_host_alert_history.go` | Labels de bucket encurtados (`02 Jan`) |
| `collect_host_alert_history.go` | Campos `TotalProblems`, `TotalResolved`, `TotalEvents` |

---

### Fase B — Docker e runtime

| Problema | Solução |
|----------|---------|
| Container em restart loop (`exec format error`) | **Dockerfile multi-stage**: build estático Go → Alpine |
| Crash por variáveis `DB_*` sem Postgres | Variáveis comentadas no `docker-compose.yml`; profile `db` opcional |
| Go desatualizado (1.18) | Atualizado para **Go 1.22** |

---

### Fase C — UI: aba "Análise por Host"

Consolidação da experiência de filtro por host:

| Antes | Depois |
|-------|--------|
| Info repetida em banner, sidebar, resumo | Sidebar: uma linha `Filtro ativo: Nome · N dias` |
| Conteúdo espalhado no resumo | Aba dedicada **`tab-host`** ("Análise por Host") |
| Gráficos inline sem tema | Helpers centralizados em `script.js` |
| Gráfico estreito no centro | Fix CSS: `.report-main canvas` limitava gauges **e** charts host |

#### Arquivos principais
- `app/cmd/app/main.go` — HTML da aba host (depois extraído)
- `app/web/static/script.js` — `zbxInitHostAlertChart`, `zbxInitHostMetricChart`, `zbxResizeChartsIn`
- `app/web/static/custom.css` — `host-focus-panel`, containers de gráfico, grid de métricas
- `app/web/locales/*/messages.json` — chaves `tabs.host`, `host_focus.*`, `chart.*`

#### Funcionalidades da aba host
- KPIs: Problemas, Resolvidos, Eventos
- Gráfico de barras: histórico de alertas por período
- Gráficos de linha: CPU, memória, rede com marcadores de alerta
- Export PNG individual e **Exportar todos**
- Auto-abertura da aba quando filtro de host está preenchido
- Empty states para eventos e métricas ausentes
- Avisos (`warnings`) quando coleta parcial falha

---

### Fase D — Gráficos profissionais e full-width

| Implementação | Detalhe |
|---------------|---------|
| Tema unificado | Cores, tooltip, legenda, eixos em `script.js` |
| Tema claro | `chartTheme()` adapta cores quando `body.theme-light` |
| Lazy init | `zbxQueueHostChart` + `zbxFlushHostCharts` — charts só renderizam com aba visível |
| Resize automático | Ao abrir `tab-host` e após `renderReport()` |
| Export HTML | Reinicializa gauges **e** gráficos host no documento exportado |
| Fix CSS | `.report-main .summary-gauges canvas[data-total]` — gauges não afetam charts host |

---

### Fase E — Backend unificado e arquitetura

#### Coletor unificado por host
- Novo `CollectHostFocusReport()` em `collect_host_focus.go`
- **Uma** resolução de host + **um** `event.get` (metade das chamadas API)
- Deriva histórico de alertas e métricas detalhadas do mesmo conjunto de eventos

#### Resolução de host melhorada
- Prioridade: `hostid` exato → nome técnico/visível exato → partial único
- Erro explícito quando **múltiplos hosts** correspondem ao filtro
- Novo `ResolveHostsByFilter()` retorna status `ok | ambiguous | not_found | empty`

#### Extração de código de `main.go`

| Pacote | Responsabilidade |
|--------|------------------|
| `internal/report/hosttab.go` | HTML da aba Análise por Host (~170 linhas extraídas) |
| `internal/zabbix/client.go` | Cliente JSON-RPC, trends, history, item lookup (~500 linhas extraídas) |
| `internal/server/auth.go` | Middleware `APP_API_KEY` |
| `internal/collector/host_types.go` | Tipos compartilhados de host/métricas |

**Resultado:** `main.go` reduzido de ~4.900 para ~4.300 linhas.

#### Erros silenciosos corrigidos
- Falhas em `trend.get` / `history.get` agora geram **warnings** visíveis na UI
- Semântica unificada: host não encontrado retorna `error` em todos os coletores

---

### Fase F — Seletor de host ambíguo (UI)

Fluxo ao gerar relatório com filtro de host:

```
Usuário preenche filtro → POST /api/hosts/resolve
  ├─ ok        → usa nome exato do host
  ├─ ambiguous → modal com lista de hosts para escolher
  ├─ not_found → mensagem de erro
  └─ empty     → gera relatório geral (sem filtro host)
```

**Arquivos:** `index.html` (modal), `script.js` (`showHostPicker`), `custom.css`, i18n `host_picker.*`

---

### Fase G — Segurança e operação

| Recurso | Implementação |
|---------|---------------|
| `APP_API_KEY` | Protege `POST /api/start`, `POST /api/hosts/resolve`, `DELETE /api/reports*` |
| Header `X-API-Key` | Enviado automaticamente pelo frontend quando configurado |
| `GET /api/config` | Retorna `api_key_required` e `tls_verify` |
| `ZABBIX_TLS_VERIFY` | `true` habilita verificação TLS nas chamadas Zabbix (padrão: skip verify) |
| TTL de tasks | Tasks em memória expiram após **2 horas** |
| `MAX_CONCURRENT` | Alias legado `MAX_CCONCURRENT` mantido no compose |

Documentação em `README.md` (seção Security configuration) e comentários no `docker-compose.yml`.

---

### Fase H — i18n e formulário

| Chave / campo | Descrição |
|---------------|-----------|
| `form.host_filter_label` | Label do filtro de host |
| `form.host_filter_placeholder` | Placeholder do filtro |
| `form.alert_days_label` | Período em dias |
| `form.api_key_label` | Chave de API (quando `APP_API_KEY` ativa) |
| `host_focus.no_metrics` | Empty state de métricas |
| `host_picker.*` | Modal de seleção de host |
| `progress.collecting_host_focus` | Mensagem de progresso unificada |

---

### Fase I — Testes e CI

| Item | Detalhe |
|------|---------|
| Testes existentes | 12+ arquivos `*_test.go` em `internal/collector` |
| Novos testes | `resolve_host_test.go` (exact, partial, ambiguous, not found) |
| CI | `.github/workflows/test.yml` — `go test ./...` em push/PR |
| Build | `go build` e Docker multi-stage validados |

---

## 4. Arquitetura atual

```
┌─────────────────────────────────────────────────────────────┐
│                     Frontend (Browser)                       │
│  index.html → script.js → Chart.js → i18n (pt_BR/en_US)     │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTP (Gin)
┌──────────────────────────▼──────────────────────────────────┐
│  cmd/app/main.go                                             │
│  ├─ Handlers: /, /api/start, /api/hosts/resolve, /api/...   │
│  ├─ generateZabbixReport() — montagem HTML das abas         │
│  └─ Wrappers finos → zbxClient, collectors                  │
└───────┬──────────────────────┬──────────────────────────────┘
        │                      │
┌───────▼──────────┐  ┌────────▼─────────────────────────────┐
│ internal/zabbix  │  │ internal/collector                    │
│ Client JSON-RPC  │  │ CollectHostFocusReport, CollectHosts, │
│ trends, history  │  │ CollectItems, CollectTriggers, ...  │
└───────┬──────────┘  └────────┬─────────────────────────────┘
        │                      │
        └──────────┬───────────┘
                   │ JSON-RPC
        ┌──────────▼───────────┐
        │   Zabbix API         │
        │   (6.x / 7.x / 8.x)  │
        └──────────────────────┘

Pacotes auxiliares:
  internal/report/hosttab.go  → HTML aba Análise por Host
  internal/server/auth.go     → Middleware APP_API_KEY
```

---

## 5. Funcionalidades por área

### Relatório geral (base + evolução oficial)
- Resumo com KPIs e gauges (hosts desabilitados, items não suportados)
- Abas: Server, Proxys, Items, Templates, Triggers, Users, Alertas, Recomendações
- Tabelas com busca, ordenação e paginação (`script.js`)
- Export HTML standalone e impressão/PDF
- Tema claro/escuro
- i18n PT/EN

### Análise por host (implementação local)
- Filtro opcional no formulário (`host_filter` + `alert_days`)
- Coleta unificada de eventos e métricas
- Aba dedicada com stats, gráficos e export PNG
- Seletor modal para hosts ambíguos
- Warnings quando métricas específicas não são encontradas

### API e segurança (implementação local)
- Resolução de host antes de gerar relatório
- API key opcional para endpoints sensíveis
- TLS configurável para Zabbix

---

## 6. Variáveis de ambiente

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `APP_DEBUG` | `false` | Logs verbose da API Zabbix |
| `API_TIMEOUT_SECONDS` | `60` | Timeout HTTP para chamadas Zabbix |
| `CHECKTRENDTIME` | `15d` | Janela de trends/history (`15d`, `12h`, `90m`) |
| `MAX_CONCURRENT` | `4` | Goroutines paralelas para API (alias: `MAX_CCONCURRENT`) |
| `ZABBIX_SERVER_HOSTID` | — | HostID do Zabbix Server (processos/pollers) |
| `APP_API_KEY` | _(vazio)_ | Protege rotas sensíveis; header `X-API-Key` |
| `ZABBIX_TLS_VERIFY` | `false` | `true` = verificar certificado TLS do Zabbix |
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` | — | PostgreSQL (profile `db` no compose) |

---

## 7. API HTTP

| Método | Rota | Auth | Descrição |
|--------|------|------|-----------|
| GET | `/` | — | Interface web |
| GET | `/api/config` | — | Config pública (`api_key_required`, `tls_verify`) |
| POST | `/api/hosts/resolve` | API Key* | Resolve filtro de host |
| POST | `/api/start` | API Key* | Inicia geração de relatório |
| GET | `/api/progress/:id` | — | Progresso da task |
| GET | `/api/report/:id` | — | HTML do relatório gerado |
| GET | `/api/db-status` | — | Postgres habilitado? |
| GET | `/api/reports` | — | Lista relatórios salvos |
| GET | `/api/reportdb/:id` | — | Abre relatório do banco |
| DELETE | `/api/reportdb/:id` | API Key* | Exclui relatório |
| DELETE | `/api/reports` | API Key* | Exclui todos |

\* Requer `X-API-Key` apenas quando `APP_API_KEY` está definida.

### Exemplo: resolver host

```bash
curl -s -X POST http://localhost:8080/api/hosts/resolve \
  -H 'Content-Type: application/json' \
  -d '{
    "zabbix_url": "https://zabbix.example.com",
    "zabbix_token": "YOUR_TOKEN",
    "host_filter": "web"
  }'
```

Resposta ambígua:
```json
{
  "status": "ambiguous",
  "hosts": [
    {"hostid": "1001", "name": "web01"},
    {"hostid": "1002", "name": "web02"}
  ]
}
```

---

## 8. Testes e CI

```bash
cd app
go test ./...
go build -mod=vendor ./cmd/app
```

```bash
# Docker
docker compose up --build -d
curl http://localhost:8080/api/config
```

Workflows GitHub Actions:
- `docker-publish.yml` — build e push da imagem Docker
- `test.yml` — `go test ./...` em push/PR na pasta `app/`

---

## 9. Como validar

### Checklist rápido

- [ ] `docker compose up --build -d` sobe sem restart loop
- [ ] http://localhost:8080 carrega o formulário
- [ ] Relatório **sem filtro** gera todas as abas padrão
- [ ] Relatório **com filtro de host** abre aba **Análise por Host** automaticamente
- [ ] Gráfico de alertas ocupa **largura total** do container
- [ ] Filtro ambíguo (ex.: `web`) abre **modal de seleção**
- [ ] **Exportar todos** baixa PNGs das métricas
- [ ] Export HTML inclui gráficos da aba host
- [ ] Tema claro deixa charts legíveis
- [ ] Com `APP_API_KEY` definida, formulário exige chave de API

### Comandos

```bash
cd /caminho/para/zabbix-eay-visual
docker compose up --build -d
# Acesse http://localhost:8080
# Ctrl+Shift+R para limpar cache do navegador
```

---

## 10. Próximos passos sugeridos

Itens **implementados nesta branch (jul/2026)**:

| Item | Status |
|------|--------|
| KPIs clicáveis + filtro por tipo na tabela de alertas | ✅ |
| Contagem de alertas "Outros" | ✅ |
| Aviso de truncamento (10.000 eventos) | ✅ |
| Coluna de duração do problema | ✅ |
| Export CSV da tabela de alertas | ✅ |
| Classificação configurável (`web/config/alert_rules.json`) | ✅ |
| Seleção manual de itens CPU/mem/rede (formulário) | ✅ |
| Comparar dois hosts (aba dedicada) | ✅ |
| Rate limit em `POST /api/start` (`RATE_LIMIT_START_RPM`) | ✅ |
| Postgres `depends_on` no docker-compose | ✅ |
| Testes: auth, rate limit, hosttab, duração, buildHostMeta | ✅ |
| Legenda do item usado nos gráficos de métricas | ✅ |

Itens **ainda não implementados** que podem ser priorizados:

| Prioridade | Item |
|------------|------|
| Alta | Validação end-to-end com ambiente Zabbix real |
| Média | Extrair mais abas de `main.go` → `internal/report/` |
| Média | Endpoint `/api/hosts/:id/items` para picker visual de itens |
| Baixa | Autenticação de usuário (além de API key) |

---

## Mapa de arquivos modificados/criados (resumo)

### Novos arquivos
```
app/internal/collector/collect_host_focus.go
app/internal/collector/collect_host_alert_history.go
app/internal/collector/collect_host_detailed_report.go
app/internal/collector/host_types.go
app/internal/collector/resolve_host.go
app/internal/collector/resolve_host_test.go
app/internal/report/hosttab.go
app/internal/zabbix/client.go
app/internal/server/auth.go
.github/workflows/test.yml
README_ATUALIZACOES.md  (este arquivo)
```

### Arquivos significativamente alterados
```
app/cmd/app/main.go
app/web/static/script.js
app/web/static/custom.css
app/web/templates/index.html
app/web/locales/pt_BR/messages.json
app/web/locales/en_US/messages.json
app/Dockerfile
app/go.mod
docker-compose.yml
README.md
```

---

## Referências

- [README.md](README.md) — documentação principal (inglês)
- [README_pt_br.md](README_pt_br.md) — documentação principal (português)
- [CHANGELOG.md](CHANGELOG.md) — releases oficiais versionadas
- [Documentação MkDocs](https://bernardolankheet.github.io/zabbix-easy)

---

Ver também: [DEPLOY_SERVIDOR.md](DEPLOY_SERVIDOR.md) — como substituir a versão antiga no servidor de produção.

*Documento gerado com base no histórico do repositório e nas implementações da branch de desenvolvimento local `zabbix-eay-visual`. Última atualização: julho/2026.*
