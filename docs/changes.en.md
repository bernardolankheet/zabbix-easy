# What's New / Updates

Date: 16/03/2026

Summary of recent changes:

- UI: added a highlighted recommendations list (yellow boxes) for quick proxy actions. CSS classes: `.rec-highlight-list` and `.rec-highlight-item` in `app/web/static/style.css`.
- Per-proxy recommendations now generate dynamic code boxes (one block per proxy), using the automatically detected parameter (e.g. `StartPollers`, `StartHTTPPollers`, `StartPollersUnreachable`) instead of a hardcoded value. Implementation in `app/cmd/app/main.go`.
- Internationalization: new keys added in `app/web/locales/pt_BR/messages.json` and `app/web/locales/en_US/messages.json`:
  - `fix.proxy_increase_hint` — comment text inside snippets (`increase until the avg drops below 60%`).
  - `fix.proxy_highlight_process_title`, `fix.proxy_highlight_offline_title`, `fix.proxy_highlight_async_title` — titles for highlighted blocks.
- Backend: fixes to `hostid` resolution for process items — hostid is now captured only from items whose key starts with `process.`; removed debug logs.

Main modified files:
- `app/cmd/app/main.go` — HTML report generation logic and hostid/process capture.
- `app/web/static/style.css` — new classes for the highlighted list.
- `app/web/locales/pt_BR/messages.json` and `en_US/messages.json` — new i18n keys.

Notes for operators and developers:
- To test locally, rebuild the image and run the service (e.g. `docker compose up -d --build`) and generate a report to see the new per-proxy recommendations.
