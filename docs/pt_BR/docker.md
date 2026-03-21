---
title: "Docker"
lang: pt_BR
---

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
| `MAX_CCONCURRENT` | `4` | Goroutines paralelas para chamadas à API do Zabbix. Reduzir para `2`–`3` se o Zabbix ficar lento ou retornar timeouts. |
| `API_TIMEOUT_SECONDS` | `60` | Timeout em segundos de cada requisição HTTP à API do Zabbix. Aumentar para `90`–`120` em ambientes com muitos hosts/itens ou latência alta. |
| `APP_DEBUG` | _(vazio)_ | `1` para logs detalhados de cada requisição à API. |
| `DB_HOST` | _(vazio)_ | Host do PostgreSQL. **Se vazio, persistência desativada e card DB oculto.** |
| `DB_PORT` | `5432` | Porta do PostgreSQL. |
| `DB_USER` | `postgres` | Usuário do banco. |
| `DB_PASSWORD` | `postgres` | Senha do banco. |
| `DB_NAME` | `zabbix_report` | Nome do banco de dados. |

### Variáveis do `postgres`

| Variável | Valor padrão |
|----------|--------------|
| `POSTGRES_DB` | `zabbix_report` |
| `POSTGRES_USER` | `postgres` |
| `POSTGRES_PASSWORD` | `postgres` |

---

## Profiles do Docker Compose

| Profile | Serviços iniciados | Quando usar |
|---------|--------------------|-------------|
| _(nenhum)_ | `go-app` | Uso sem banco — avaliação rápida |
| `db` | `go-app` + `postgres` | Uso com persistência de relatórios |

> **Nota:** o serviço `go-app` também possui `profiles: ["app"]` no `docker-compose.yml`.
> Para subir apenas o `go-app` pelo profile: `docker compose --profile app up -d`.
> Para subir tudo junto: `docker compose --profile app --profile db up --build -d`.

---

## Comandos úteis

```bash
# Subir sem banco (padrão)
docker compose up -d

# Subir com banco
docker compose --profile db up --build -d

# Ver logs do app
docker logs -f go-zabbix-app

# Ver logs do postgres
docker logs -f go-zabbix-postgres

# Parar tudo (preserva volume de dados)
docker compose --profile db down

# Parar e remover volume de dados
docker compose --profile db down -v

# Acessar o banco diretamente
docker exec -it go-zabbix-postgres psql -U postgres -d zabbix_report
```

---

## Porta exposta

| Serviço | Porta host | Porta container |
|---------|------------|-----------------|
| `go-app` | `8080` | `8080` |
| `postgres` | `5433` | `5432` |

> O postgres expõe a porta `5433` no host (evita conflito com um postgres local na `5432`).

---

## Persistência de dados

O volume `go_zabbix_postgres_data` é nomeado explicitamente para sobreviver a `docker compose down`.
Só é removido com `docker compose down -v` ou `docker volume rm go_zabbix_postgres_data`.