---
title: "Instalação"
lang: pt_BR
---

# Instalação

## Requisitos

- Docker

---

## Opção 1 — Docker Run (mais simples)

Ideal para uso rápido, e sem necessidade de utilizar banco de dados.

```bash
docker run -d \
  --name zabbix-easy \
  -p 8080:8080 \
  -e MAX_CCONCURRENT=10 \
  -e ZABBIX_SERVER_HOSTID=10084 \
  -e CHECKTRENDTIME=15d \
  bernardolankheet/zabbix-easy:latest
```

> **Como encontrar o `ZABBIX_SERVER_HOSTID`:** Acesse o frontend do Zabbix → **Data Collection** → pesquise pelo host "Zabbix Server" → abra o host e verifique o ID na URL. O valor padrão é `10084`.

Acesse a interface em `http://localhost:8080`.

---

## Opção 2 — Docker Compose (sem banco de dados)

Relatórios ficam disponíveis apenas na sessão atual. Ao reiniciar o container, os relatórios anteriores são perdidos.

```bash
git clone https://github.com/bernardolankheet/zabbix-easy.git
cd zabbix-easy
docker compose up --build -d
```

Acesse a interface em `http://localhost:8080`.

---

## Opção 3 — Docker Compose (com banco de dados PostgreSQL)

Persiste todos os relatórios gerados. Permite reabrir, comparar e excluir relatórios anteriores.

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

O profile `db` é necessário pois o serviço `postgres` **só é iniciado quando explicitamente solicitado**.

Acesse a interface em `http://localhost:8080`.

---

## Opção 4 — Helm (Kubernetes)

Use o chart incluído em `helm/zabbix-easy`. Exemplos abaixo instalam o chart localmente no cluster atual.

Instalação básica (chart local):

```bash
helm upgrade --install zabbix-easy ./helm/zabbix-easy \
  --namespace zabbix-easy --create-namespace
```

Definir imagem e variáveis de aplicação via `--set`:

```bash
helm upgrade --install zabbix-easy ./helm/zabbix-easy \
  -n zabbix-easy --create-namespace \
  --set image.tag=latest \
  --set env.ZABBIX_SERVER_HOSTID=10084 \
  --set env.CHECKTRENDTIME=15d
```

Usar `values.yaml` customizado (recomendado para produção):

```bash
# criar my-values.yaml com overrides (ex: ingress host, postgres.enabled: false, etc.)
helm upgrade --install zabbix-easy ./helm/zabbix-easy \
  -n zabbix-easy --create-namespace -f my-values.yaml
```

Exemplos úteis:

- Desabilitar PostgreSQL interno e usar um banco externo:

```yaml
postgres:
  enabled: false

  DB_HOST: my-postgres-host
  DB_USER: myuser
  DB_PASSWORD: mypass
```

- Ajustar Ingress (edite `my-values.yaml` para configurar `ingress.rules`/`host`):

```yaml

  enabled: true
  rules:
    - host: zabbix-easy.exemple.local
      path: /
  tlsenabled: true
```

Após a instalação, acesse o serviço via Ingress/LoadBalancer ou execute um port-forward:

```bash
kubectl port-forward svc/zabbix-easy -n zabbix-easy 8080:8080
# acessar http://localhost:8080
```

---

## Variáveis de ambiente principais

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `ZABBIX_SERVER_HOSTID` | `10084` | ID do host "Zabbix Server" no frontend do Zabbix. Usado para coleta de métricas de performance. |
| `CHECKTRENDTIME` | `30d` | Janela de tempo para análise de trends. Aceita `d` (dias), `h` (horas). Ex: `15d`, `24h`. |
| `MAX_CCONCURRENT` | `4` | Goroutines paralelas para chamadas à API do Zabbix. Reduzir para `2`–`3` se o Zabbix ficar lento. |
| `API_TIMEOUT_SECONDS` | `60` | Timeout em segundos por requisição. Aumentar para `90`–`120` em ambientes lentos. |
| `APP_DEBUG` | _(vazio)_ | `true` para logs detalhados de cada requisição à API. |
| `DB_HOST` | _(vazio)_ | Host do PostgreSQL. **Se vazio, persistência desativada.** |


