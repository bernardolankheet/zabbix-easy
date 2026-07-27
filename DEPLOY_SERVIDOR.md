# Deploy no servidor — substituir versão antiga

Este guia explica como colocar **esta versão** no lugar da instalação antiga do Zabbix Easy Report.

---

## Pré-requisitos no servidor

- Docker instalado (20.10+)
- Docker Compose v2 (`docker compose`) ou `docker-compose`
- Porta **8080** livre (ou outra definida em `.env`)
- Git **ou** acesso para copiar os arquivos (rsync/scp)

---

## Opção A — Git (recomendado)

### 1. Conectar no servidor

```bash
ssh usuario@SEU_SERVIDOR
```

### 2. Backup da instalação antiga (opcional)

```bash
# Se usava docker run
docker stop zabbix-easy 2>/dev/null; docker rm zabbix-easy 2>/dev/null

# Se tinha pasta antiga
sudo mv /opt/zabbix-easy /opt/zabbix-easy.old.$(date +%Y%m%d) 2>/dev/null || true
```

### 3. Clonar ou atualizar o repositório

**Primeira vez:**

```bash
sudo mkdir -p /opt/zabbix-easy-report
sudo chown $USER:$USER /opt/zabbix-easy-report
cd /opt/zabbix-easy-report
git clone https://github.com/bernardolankheet/zabbix-easy.git .
# Se suas alterações estão só na máquina local, use rsync (Opção B) ou push para um fork/remoto.
```

**Atualizar pasta existente:**

```bash
cd /opt/zabbix-easy-report   # ou o caminho onde está o projeto
git fetch origin
git checkout dev             # ou a branch com as melhorias
git pull
```

### 4. Configurar ambiente

```bash
cp .env.example .env
nano .env   # ajuste ZABBIX_SERVER_HOSTID, APP_PORT, APP_API_KEY, etc.
```

### 5. Executar deploy

```bash
chmod +x scripts/deploy-server.sh
./scripts/deploy-server.sh
```

**Com PostgreSQL:**

```bash
./scripts/deploy-server.sh --with-db
```

### 6. Validar

```bash
curl http://localhost:8080/api/config
# Abra no navegador: http://IP_DO_SERVIDOR:8080
```

---

## Opção B — Copiar da sua máquina (rsync)

Use quando as alterações **ainda não estão no GitHub** (como neste workspace local).

**Na sua máquina de desenvolvimento:**

```bash
cd /home/michel.quaresma/Documentos/zabbix-eay-visual

rsync -avz --delete \
  --exclude '.git' \
  --exclude 'app/app' \
  --exclude 'app/go-zabbix-app' \
  ./ usuario@SEU_SERVIDOR:/opt/zabbix-easy-report/
```

**No servidor:**

```bash
ssh usuario@SEU_SERVIDOR
cd /opt/zabbix-easy-report
cp -n .env.example .env   # -n = não sobrescreve .env existente
nano .env
chmod +x scripts/deploy-server.sh
./scripts/deploy-server.sh
```

---

## Opção C — Apenas Docker (sem git no servidor)

**Use quando o servidor NÃO acessa o Docker Hub** (erro `i/o timeout` em `registry-1.docker.io`).

### Na sua máquina (com internet)

```bash
cd /home/michel.quaresma/Documentos/zabbix-eay-visual
chmod +x scripts/export-image.sh
./scripts/export-image.sh

scp zabbix-easy-report.tar.gz root@x-oc-grafana-prd:/opt/zabbix-easy-report/
```

### No servidor (sem build)

```bash
cd /opt/zabbix-easy-report
chmod +x scripts/deploy-offline.sh
./scripts/deploy-offline.sh
```

O script carrega a imagem, sobe o container e valida `/api/config` — **sem baixar nada do Docker Hub**.

---

## Opção D — Build manual da imagem (alternativa)

**Build local e envio da imagem** (servidor sem código fonte):

Na máquina de build:

```bash
cd app
docker build -t zabbix-easy-report:2026 .
docker save zabbix-easy-report:2026 | gzip > zabbix-easy-report.tar.gz
scp zabbix-easy-report.tar.gz usuario@SEU_SERVIDOR:/tmp/
```

No servidor:

```bash
docker load < /tmp/zabbix-easy-report.tar.gz
docker rm -f zabbix-easy zabbix-easy-report go-zabbix-app 2>/dev/null || true
docker run -d --name zabbix-easy-report \
  --restart unless-stopped \
  -p 8080:8080 \
  -e ZABBIX_SERVER_HOSTID=10084 \
  -e MAX_CONCURRENT=10 \
  -e CHECKTRENDTIME=15d \
  -e APP_DEBUG=false \
  zabbix-easy-report:2026
```

---

## Diferenças: versão antiga vs esta versão

| Aspecto | Versão antiga | Esta versão |
|---------|---------------|-------------|
| Container | `zabbix-easy` / binário glibc | Multi-stage Alpine, Go 1.22 |
| Filtro por host | Limitado / com bugs | Aba **Análise por Host**, gráficos, export |
| Host ambíguo | Erro genérico | Modal de seleção |
| API | Sem proteção | `APP_API_KEY` opcional |
| Frontend | Fixo na imagem | `docker-compose.prod.yml` usa imagem completa |

---

## Variáveis importantes (`.env`)

| Variável | Obrigatório | Descrição |
|----------|-------------|-----------|
| `ZABBIX_SERVER_HOSTID` | Sim | HostID do Zabbix Server no seu ambiente |
| `APP_PORT` | Não | Porta no host (padrão 8080) |
| `APP_API_KEY` | Recomendado | Protege geração de relatório |
| `MAX_CONCURRENT` | Não | Paralelismo API (padrão 10) |
| `CHECKTRENDTIME` | Não | Janela de trends (padrão 15d) |

---

## Rollback (voltar à versão antiga)

Se manteve backup:

```bash
docker compose -f docker-compose.prod.yml down
cd /opt/zabbix-easy.old.YYYYMMDD
docker compose up -d   # ou o comando que usava antes
```

---

## Problemas comuns

| Sintoma | Solução |
|---------|---------|
| `connection refused` na 8080 | `docker logs zabbix-easy-report` |
| Container reiniciando | Verifique binário (use rebuild: `./scripts/deploy-server.sh`) |
| `i/o timeout` no Docker Hub | Servidor sem internet ao registry — use **Opção C** (`export-image.sh` + `deploy-offline.sh`) |
| Relatório sem aba host | Confirme que preencheu **Host para histórico** no formulário |
| API 401 | Configure `X-API-Key` no formulário se `APP_API_KEY` estiver ativa |

---

## Checklist pós-deploy

- [ ] http://servidor:8080 abre o formulário
- [ ] Relatório gera sem erro
- [ ] Filtro de host abre aba **Análise por Host**
- [ ] `ZABBIX_SERVER_HOSTID` correto para seu ambiente
- [ ] `APP_DEBUG=false` em produção
- [ ] Firewall liberado na porta escolhida

---

Ver também: [README_ATUALIZACOES.md](README_ATUALIZACOES.md) — histórico completo das mudanças.
