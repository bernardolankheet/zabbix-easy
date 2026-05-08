[![CI - Docker](https://github.com/bernardolankheet/zabbix-easy/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/bernardolankheet/zabbix-easy/actions/workflows/docker-publish.yml)
[![GitHub release](https://img.shields.io/github/v/release/bernardolankheet/zabbix-easy)](https://github.com/bernardolankheet/zabbix-easy/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/bernardolankheet/zabbix-easy)](https://hub.docker.com/r/bernardolankheet/zabbix-easy)
[![License](https://img.shields.io/github/license/bernardolankheet/zabbix-easy)](LICENSE)

# Zabbix Easy Report

Zabbix Easy é uma ferramenta open-source que analisa um ambiente Zabbix via API e gera um relatório HealthCheck conciso com recomendações práticas para melhorar desempenho, confiabilidade e manutenção.

Compatibilidade Zabbix:
- 6.0
- 6.4
- 7.0
- 7.2
- 7.4
- 8.0 (experimental)

## Resumo rápido
- Linguagem backend: Go
- Frontend: HTML/CSS/JS (gerado pelo backend)
- Documentação: MkDocs (pasta `docs/`)

## Estrutura do projeto
- `app/cmd/app` — backend em Go que coleta dados via Zabbix API e gera o HTML do relatório
- `app/web` — recursos estáticos (templates, i18n, CSS, JS)
- `docs/` — documentação do projeto (MkDocs)
- `app/internal/collector` — coletores para API Zabbix.

## Funcionalidades principais
- Coleta e agregação de métricas via Zabbix API
- Análises: itens não suportados, itens sem template, pollers/processos do server e proxys, trends, LLD
- Recomendações automatizadas com snippets de correção
- Exportação para HTML/PDF
- Persistência opcional de relatórios (Postgres)

## Início rápido — executar localmente

### 1) Usando Docker (mais simples):

```bash
docker run -d --name zabbix-easy -p 8080:8080 -e MAX_CCONCURRENT=10 -e ZABBIX_SERVER_HOSTID=10084 -e CHECKTRENDTIME=15d bernardolankheet/zabbix-easy:latest
# open http://localhost:8080
```

### 2) Rodando com persistência de dados (Postgres):

```bash
docker compose --profile db up --build -d
docker run -d --name zabbix-easy -p 8080:8080 -e MAX_CCONCURRENT=10 -e ZABBIX_SERVER_HOSTID=10084 -e CHECKTRENDTIME=15d bernardolankheet/zabbix-easy:latest
# Acesse http://localhost:8080
```

## Documentação
- [https://bernardolankheet.github.io/zabbix-easy](https://bernardolankheet.github.io/zabbix-easy)

## Contribuição
- Abra issues e PRs. Veja `docs/pt_BR/contribution.md` para orientações de i18n, desenvolvimento e como rodar a documentação localmente.

## Contato e licença
- Repositório: [https://github.com/bernardolankheet/zabbix-easy](https://github.com/bernardolankheet/zabbix-easy)
- Licença: veja `LICENSE`

## Notas
- Para detalhes das novas funcionalidades e mudanças veja `CHANGELOG.md`.
