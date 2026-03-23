# Zabbix Easy Report

Zabbix Easy é uma ferramenta open-source que analisa um ambiente Zabbix via API e gera um relatório HealthCheck com recomendações práticas para melhorar desempenho, confiabilidade e manutenção.

Compatibilidade: testado e funcionando em Zabbix 6.0, 6.4 e 7.0.

Resumo rápido
- Linguagem backend: Go
- Frontend: HTML/CSS/JS (gerado pelo backend)
- Documentação: MkDocs (pasta `docs/`)

Principais componentes
- `app/cmd/app` — backend em Go que coleta dados via Zabbix API e gera o HTML do relatório
- `app/web` — recursos estáticos (templates, i18n, CSS, JS)
- `docs/` — documentação do projeto (MkDocs)

Funcionalidades principais
- Coleta e agregação de métricas via Zabbix API
- Análises: itens não suportados, itens sem template, pollers/processos do server e proxys, trends, LLD
- Recomendações automatizadas com snippets de correção
- Exportação para HTML/PDF
- Persistência opcional de relatórios (Postgres)

Rápido tutorial — executar localmente
1) Usando Docker (mais simples):

```bash
docker run -d --name zabbix-easy -p 8080:8080 \
  -e MAX_CCONCURRENT=10 \
  -e ZABBIX_SERVER_HOSTID=10084 \
  -e CHECKTRENDTIME=15d \
  bernardolankheet/zabbix-easy:latest
# abra http://localhost:8080
```

2) Rodando localmente (desenvolvimento):

```bash
# compilar
cd app/cmd/app
go build -o zbx-easy
# executar
./zbx-easy
```

Documentação (MkDocs)
- Documentação online: (em breve)
- Para rodar localmente: veja `docs/contribuicao.md`

Contribuição
- Abra issues e PRs. Veja `docs/contribuicao.md` para orientações de i18n, desenvolvimento e como rodar a documentação localmente.

Contato e licença
- Repositório: https://github.com/bernardolankheet/zabbix-easy
- Licença: veja `LICENSE`

Notas
- Para detalhes das novas funcionalidades e mudanças veja `docs/CHANGELOG.md`.
