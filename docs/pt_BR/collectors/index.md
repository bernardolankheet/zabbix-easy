---
title: "Índice de Coletores"
lang: pt_BR
---

# Referência de Coletores

Esta seção documenta os coletores tipados em `app/internal/collector` usados pelo gerador de relatórios. Cada coletor encapsula métodos JSON-RPC do Zabbix, centraliza autenticação e parsing, e expõe uma API simples para os callers.

Coletores incluídos:

- [CollectZabbixVersion](collect_zabbix_version.md) — detecção de versão (`apiinfo.version`)
- [Authenticate](authenticate.md) — helper para `user.login`
- [CollectRawList](collect_raw_list.md) — genérico para métodos que retornam listas (`item.get`, `history.get`, `trend.get`)
- [CollectCount](collect_count.md) — consultas com `countOutput:true`
- [CollectItemByKey](collect_item_by_key.md) — encontra um item por `key_` exato
- [CollectProcessItemsBulk](collect_process_items_bulk.md) — descoberta bulk de processos (server)
- [CollectHostExists](collect_host_exists.md) — verificação de existência de host
- [CollectProxiesList](collect_proxies_list.md) — lista leve de proxies para resumos
 - [CollectMediaTypes](collect_mediatypes.md) — retorna `mediatypeid` e `name` para os tipos de mídia configurados

Clique em um coletor acima para abrir sua documentação e exemplos.
