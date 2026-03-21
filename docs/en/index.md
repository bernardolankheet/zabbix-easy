---
title: "Zabbix Easy HealthCheck Report"
lang: en_US
---

# Zabbix Easy HealthCheck Report

Modern project to generate Zabbix HealthCheck reports using Go and PostgreSQL.

## Components
- Go backend: API, workers, Zabbix data collection
- PostgreSQL: temporary storage for reports

## Flow
1. User provides Zabbix URL/token via the web UI
2. Workers collect data and store it in PostgreSQL
3. Report is generated and displayed in the UI

## Changelog
See the project changelog for recent changes: https://github.com/bernardolankheet/zabbix-easy/blob/main/docs/CHANGELOG.md

