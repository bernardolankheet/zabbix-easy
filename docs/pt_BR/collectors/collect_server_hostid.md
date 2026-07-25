---
title: "CollectServerHostId"
lang: pt_BR
---

# `CollectServerHostId`

Descobre o hostid do host que contém os itens de processo interno do Zabbix Server (`zabbix[process,...]`), para que o relatório não dependa de um id fixo no código.

Uso

- Assinatura: `CollectServerHostId(apiUrl, token string, req ApiRequester) (string, error)`
- Método Zabbix: `item.get` com `search: {key_: "zabbix[process,"}`, `monitored: true`, `selectHosts`

Observações

- Hostid é atribuído por instalação. O `10084` histórico só é o host "Zabbix server" numa instalação **nova**; numa instalação antiga ou migrada ele aponta para outro host qualquer e a aba do Server sai inteira vazia.
- Proxies também têm itens `zabbix[process,...]`, então vence o host com mais processos **exclusivos de servidor** (escalator, alerter, alert manager, service manager, lld manager, report manager, proxy poller), com desempate pelo total de itens de processo. Esses nomes são estáveis do Zabbix 6.0 ao 8.0.
- Devolve `""` sem erro quando nada casa — o chamador mantém o que veio de `ZABBIX_SERVER_HOSTID`.
- `ZABBIX_SERVER_HOSTID` continua sendo o override explícito e dispensa esta chamada. Se o override não casar nenhum item de processo, a descoberta roda como fallback e o resultado dela prevalece.
