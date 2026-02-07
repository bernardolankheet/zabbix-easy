# Go Zabbix HealthCheck Report

Este projeto é uma solução moderna e performática para geração de relatórios HealthCheck do Zabbix, utilizando Go, RabbitMQ e Postgres.

## Componentes
- **Go Backend**: API, workers e lógica de coleta/processamento Zabbix
- **RabbitMQ**: Orquestração de tarefas de coleta
- **Postgres**: Armazenamento temporário dos dados coletados
- **Web UI**: Interface para entrada de URL/token, barra de progresso e relatório pronto para impressão

## Como funciona
1. O usuário informa a URL/token do Zabbix via Web UI
2. O backend Go envia tarefas de coleta para o RabbitMQ
3. Workers Go processam as tarefas, coletam dados do Zabbix e armazenam no Postgres
4. Após a coleta, o relatório é gerado e exibido na interface

## Observações
- Os dados no Postgres são temporários, usados apenas para geração do relatório
- O sistema é escalável e pronto para ambientes com grande volume de dados

---

Personalize este README conforme evoluir o projeto.



Falta:

1) total de itens desabilitados.
- Adicionar contagem de itens corrigidos no relatório final


Proxys mostram apenas os que estao comunicando, state=2
Contagem de proxys com status =1 e =0