# Zabbix Easy Report

Este projeto é uma solução moderna e performática para geração de relatórios HealthCheck do Zabbix, utilizando Go e Postgres.

## Componentes
- **Go Backend**: API, workers e lógica de coleta/processamento Zabbix
- **Postgres**: Armazenamento temporário dos dados coletados (Em devolvimento)
- **Web UI**: Interface para entrada de URL/token, barra de progresso e relatório pronto para impressão

## Como funciona
1. O usuário informa a URL/token do Zabbix via Web UI
2. O backend Go envia tarefas de coleta para o RabbitMQ
3. Workers Go processam as tarefas, coletam dados do Zabbix e armazenam no Postgres
4. Após a coleta, o relatório é gerado e exibido na interface

## Funcionalidades:
✅ Coleta de dados do Zabbix via API
✅ Autenticação via token (token incompleto ou inválido é tratado como erro);
✅ Totalizadores de itens desabilitados;
✅ Totalizadores de itens Não suportados;
✅ Verificar Pollers e Treads Zabbix Server (CHECKTRENDTIME);
✅ Top 10 Items/Hosts/Templates com mais itens Não suportados (Ofensores);
✅ Contagem de proxys, verificação por status de comunicação, queue e itens não suportados;
✅ Implementação de barra de progresso na interface;
✅ Recomendação de ações corretivas;
✅ Exportação HTML e PDF;
⬜ Banco de dados Postgres para armazenamento de dados, garantindo performance e escalabilidade;
⬜ Botão de refresh para atualizar o relatório ou consulta específica sem precisar reiniciar a coleta;
⬜ imagem do fluxo de processos do zabbix;
⬜ get em items snmp get[] e walk[], poller novo;
⬜ trends dos Pollers Proxys;
⬜ Preprocessamento de item texto com discardunchanged - https://www.zabbix.com/documentation/current/en/manual/api/reference/item/get#retrieving-items-with-preprocessing-rules;


## Observações
- Os dados no atualmente são temporários, não há retencao de dados ou cache, usados apenas para geração do relatório
- O sistema é escalável e pronto para ambientes com grande volume de dados

---