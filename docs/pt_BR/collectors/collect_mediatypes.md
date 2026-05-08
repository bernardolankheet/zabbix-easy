---
title: "CollectMediaTypes"
lang: pt_BR
---

# CollectMediaTypes

`CollectMediaTypes(apiUrl, token, req ApiRequester)` — retorna a lista de tipos de mídia configurados no Zabbix.

Descrição

Este coletor invoca o método `mediatype.get` do Zabbix API com `output: ["mediatypeid","name"]` e devolve uma lista de objetos contendo `mediatypeid` e `name`.

Uso

- Local: `app/internal/collector/collect_mediatypes.go`
- Método Zabbix: `mediatype.get`
- Parâmetros enviados: `output: ["mediatypeid","name"]`

Exemplo (pseudo-resultado):

```
[ { "mediatypeid": "1", "name": "Email" }, { "mediatypeid": "0", "name": "Script" } ]
```

Notas

- Em Zabbix, `mediatypeid` igual a `0` é usado para indicar scripts/remotes commands em algumas versões ou contextos. O relatório trata explicitamente `mediatypeid == 0` como `Script` para facilitar leitura.
- Este coletor é usado pelo novo recurso de verificação de alertas para agrupar falhas por tipo de mídia e associar mensagens de erro mais comuns.

Referência de código

- `app/internal/collector/collect_mediatypes.go` — implementação simples usando `CollectRawList`
