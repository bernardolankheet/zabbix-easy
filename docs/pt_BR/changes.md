# Novidades / Atualizações

Data: 16/03/2026

Resumo das mudanças recentes introduzidas nesta versão:

- UI: adicionada uma lista de recomendações destacada (blocos amarelos) para ações rápidas nos proxys. Classes CSS: `.rec-highlight-list` e `.rec-highlight-item` em `app/web/static/style.css`.
- Recomendações por proxy agora geram caixas de código dinâmicas (um bloco por proxy quando aplicável), usando o parâmetro detectado (por exemplo `StartPollers`, `StartHTTPPollers`, `StartPollersUnreachable`) em vez de um valor fixo. Implementação em `app/cmd/app/main.go`.
- Internacionalização: novas chaves adicionadas em `app/web/locales/pt_BR/messages.json` e `app/web/locales/en_US/messages.json`:
  - `fix.proxy_increase_hint` — texto usado no comentário dentro dos snippets (`aumente até o avg cair abaixo de 60%` / `increase until the avg drops below 60%`).
  - `fix.proxy_highlight_process_title`, `fix.proxy_highlight_offline_title`, `fix.proxy_highlight_async_title` — títulos para os blocos destacados.
- Backend: correções na resolução de `hostid` para process items — captura de `hostid` apenas a partir de items com chave começando por `process.` e proteção para não sobrescrever hostid válido; remoção de logs de debug usados no diagnóstico.

Arquivos modificados (principais):
- `app/cmd/app/main.go` — lógica de geração do HTML do relatório e captura de hostid/processos.
- `app/web/static/style.css` — novas classes para estilizar a lista destacada.
- `app/web/locales/pt_BR/messages.json` e `app/web/locales/en_US/messages.json` — novas chaves i18n.

Notas para operadores e desenvolvedores:
- Para testes locais, reconstrua a imagem e rode o serviço (ex.: `docker compose up -d --build`) e gere um relatório para visualizar as novas recomendações por proxy.
