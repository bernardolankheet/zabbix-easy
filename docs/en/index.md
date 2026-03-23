---
title: "Zabbix Easy HealthCheck Report"
lang: en_US
---

# Zabbix Easy HealthCheck Report

Project for a Zabbix HealthCheck using Go and Postgres.

## Compatibility:

Tested and working on Zabbix:

- 6.0
- 6.4
- 7.0
- 7.2
- 7.4
- 8.0 (experimental)

## Components
- Go backend: API, workers, Zabbix data collection
- PostgreSQL: temporary storage for reports

## Flow
1. User provides Zabbix URL/token via the web UI
2. Workers collect data and store it in PostgreSQL
3. Report is generated and displayed in the UI

## Changelog
See the project changelog for recent changes: https://github.com/bernardolankheet/zabbix-easy/blob/main/CHANGELOG.md

