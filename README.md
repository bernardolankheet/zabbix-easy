# Zabbix Easy Report

Zabbix Easy é um projeto opensource que utiliza a API do Zabbix, tendo seu objetivo de verificar métricas, comportamentos e configurações, para  e propor melhorias e correções em Ambientes Zabbix, a fim de melhorar a performance e a estabilidade do sistema.

Zabbix Easy é uma Interface simples, precisando apenas de um token de API para acesso ao ambiente, a ferramenta consulta a API do Zabbix e gera relatórios com dados a partir de expertises de profissionais que atuam a anos com Zabbix, facilitando a analise de administradores que possam estar iniciando na jornada de monitoramento.

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
✅ Coleta de dados do Zabbix via API;
✅ Autenticação via token (tratativa para token inválido);
✅ Totalizadores de Templates, Hosts, Items, Proxys e Usuario;
✅ Totalizadores de itens desabilitados, Habilitados, não suportados;
✅ NVPS;
✅ Analise de Pollers e Treads do Zabbix Server;
✅ Analise de totalizadores de Itens, items não suportados e queue por Proxys; (Facilitando analise das filas)
✅ Analise de Items sem templates;
✅ Analise de Items e LLDs não suportados;
✅ Analise de Items e LLDs com intervalo de coleta abaixo de 1m e 5m;
✅ Analise de Items do Tipo Texto com retenção de historico, sem a utilização de Preprocessamento Discardunchanged;
✅ Analise dos principais erros de Coleta dos Items;
✅ Top 10 Items/Hosts/Templates Ofensores do Ambiente, com mais itens Não suportados e Erros;
✅ Contagem de proxys, verificação por status de comunicação, queue e itens não suportados;
✅ Verificação se está utilizando "Asynchronous poller" no Zabbix 7;
✅ Verificação se está utilizando chaves get[] e walk[] em items SNMP;
✅ Links para integração com o Frontend do Zabbix, filtrando informações especificas;
✅ Paralelização de coleta e processamento para otimização de chamadas à API do Zabbix, melhorando significativamente peformance;
✅ Exportação HTML e PDF;
✅ Recomendação de ações corretivas;
    - Processos e Threads do Zabbix Server (recomendação para habilitar conforme a versão do Zabbix, exemplo habilitar snmp poller no Zabbix 7);
    - Sugestões de correções em Items e Templates;

Proximas Funcionalidades:
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