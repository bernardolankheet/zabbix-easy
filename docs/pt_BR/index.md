---
title: "Zabbix Easy HealthCheck Report"
lang: pt_BR
---

# Zabbix Easy HealthCheck Report

Projeto de um HealthCheck do Zabbix usando Go e Postgres.

![Report Summary](../img/screenshots/report-summary.jpg)

## Compatibilidade: 

Testado e funcionando no Zabbix:

- 6.0
- 6.4
- 7.0
- 7.2
- 7.4
- 8.0 (experimental)

## Componentes
- Go Backend: API, workers, coleta Zabbix
- Postgres: Armazenamento temporário

## Fluxo
1. Usuário informa URL/token do Zabbix via Web UI
2. Workers coletam dados e armazenam no Postgres
3. Relatório gerado e exibido na interface

## Novidades
Consulte as mudanças e notas de atualização em [Novidades](https://github.com/bernardolankheet/zabbix-easy/blob/main/docs/CHANGELOG.md).

