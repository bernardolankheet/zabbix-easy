# Zabbix Easy HealthCheck Report

Modern project to generate Zabbix HealthCheck reports using Go and Postgres.

## Components
- Go Backend: API, workers, Zabbix data collection
- Postgres: Temporary storage

## Flow
1. User provides Zabbix URL/token via Web UI
2. Workers collect data and store it in Postgres
3. Report is generated and displayed in the interface

## What's New
See the change notes in [What's New](https://github.com/bernardolankheet/zabbix-easy/blob/main/docs/CHANGELOG.md).
