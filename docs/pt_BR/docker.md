# Docker

## Modos de execução

O Zabbix Easy suporta dois modos: **sem banco** (padrão) e **com banco** onde há uma persistência de relatórios em banco os HTML são salvos em banco para fim de comparação e histórico.
Utilizo a ENV `DB_HOST` como ponto de controle, onde se estiver vazia, nenhum banco é necessário e o codigo entende que o usuário deseja rodar sem banco, ocultando o card de relatórios salvos e mantendo os relatórios apenas na sessão atual (in-memory).

```
var dbEnabled = os.Getenv("DB_HOST") != ""
```

---

## Modo 1 — Sem banco de dados (padrão)

Ideal para avaliação rápida ou ambientes onde não é necessário guardar histórico de relatórios.

```bash
docker compose up --build -d
```

- Apenas o serviço `go-app` é iniciado
- Relatórios ficam disponíveis **somente na sessão atual** (in-memory)
- O card **Relatórios Salvos** é **ocultado automaticamente** na interface
- Ao reiniciar o container, relatórios anteriores são perdidos

---

## Modo 2 — Com banco de dados PostgreSQL

Persiste todos os relatórios gerados no PostgreSQL. Permite reabrir, comparar e excluir relatórios anteriores.

### Passo 1 — Habilitar as variáveis de banco no `docker-compose.yml`

Descomente o bloco de variáveis `DB_*` no serviço `go-app`:

```yaml
environment:
  - DB_HOST=postgres
  - DB_PORT=5432
  - DB_USER=postgres
  - DB_PASSWORD=postgres
  - DB_NAME=zabbix_report
```

### Passo 2 — Subir com o profile `db`

```bash
docker compose --profile db up --build -d
```

O profile `db` é necessário porque o serviço `postgres` **só é iniciado quando explicitamente pedido**.
Sem `--profile db`, o postgres não sobe — mesmo com as variáveis `DB_*` descomentadas.

### O que acontece na primeira execução

1. O `go-app` tenta conectar ao PostgreSQL (até 15 tentativas, 2 s de intervalo)
2. Se a conexão falhar após 15 tentativas → container para com erro `[FATAL]`
3. Se conectar com sucesso → cria a tabela `reports` automaticamente (se não existir)
4. Schema antigo com colunas faltando → tabela antiga renomeada para `reports_old_<timestamp>` e nova tabela criada

---

## Referência de variáveis de ambiente

### Variáveis do `go-app`

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `ZABBIX_SERVER_HOSTID` | 10048 - Default | HostID do Zabbix Server para coleta de dados de performance. |
| `CHECKTRENDTIME` | `30d` | Janela de análise de trends. Aceita `d`, `h`, `m`. Ex: `15d`, `24h`. |
| `MAX_CCONCURRENT` | `4` | Goroutines paralelas para chamadas à API do Zabbix. Reduzir para `2`–`3` se o Zabbix ficar lento. |
| `API_TIMEOUT_SECONDS` | `60` | Timeout em segundos por requisição. Aumentar para `90`–`120` em ambientes lentos. |
| `APP_DEBUG` | _(vazio)_ | `true` para logs detalhados de cada requisição à API. |
| `DB_HOST` | _(vazio)_ | Host do PostgreSQL. **Se vazio, persistência desativada.** |
