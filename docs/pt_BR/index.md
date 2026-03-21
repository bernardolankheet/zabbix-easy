# Zabbix Easy HealthCheck Report

Projeto moderno para geração de relatórios HealthCheck do Zabbix usando Go e Postgres.

## Componentes
- Go Backend: API, workers, coleta Zabbix
- Postgres: Armazenamento temporário

## Fluxo
1. Usuário informa URL/token do Zabbix via Web UI
2. Workers coletam dados e armazenam no Postgres
3. Relatório gerado e exibido na interface

## Novidades
Consulte as mudanças e notas de atualização em [Novidades](https://github.com/bernardolankheet/zabbix-easy/blob/main/docs/CHANGELOG.md).
