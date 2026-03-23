# CHANGELOG

## [0.0.3] - 22/03/2026

### Documentation updates (22/03/2026)

- Reordered and consolidated documentation sections in both English and Portuguese (`docs/en/usage.md`, `docs/pt_BR/usage.md`): canonicalized the "Zabbix API calls" block and moved it to appear immediately before the API diagram; removed duplicate blocks.
- Added expanded documentation for the `Users` tab (based on `app/cmd/app/main.go`) in both `docs/en/usage.md` and `docs/pt_BR/usage.md`.
- Updated screenshots pages with more descriptive captions and added screenshots (`docs/en/screenshots.md`, `docs/pt_BR/screenshots.md`).
- Added a prominent compatibility note stating the app is "tested and working on Zabbix 6.0, 6.4 and 7.0" to: `README.md`, `README_pt_br.md`, `docs/index.md`, and `docs/pt_BR/usage.md`.
- Split README into English (`README.md`) and Portuguese (`README_pt_br.md`) versions; English README now the primary repo README.

These documentation edits were applied to improve clarity, remove duplicate content, and align docs with the app's UI and code.

## [0.0.2] - 21/03/2026

## New Features and Improvements

- User Guide (`tab-usuarios`):
  - Search only for the `Admin` account via `user.get` with `filter: { username: "Admin" }`.
  - Best-effort authentication test `user.login` with `Admin`/`zabbix` to detect default password; token discarded.
  - When the default password is accepted, the report displays a critical recommendation in the security section and a critical KPI.
- Recommendations (accordion):
  - Recommendations for recommendations (`details.rec-section`) now start collapsed by default; click to expand.
- Improved proxy recommendations:
  - Proxy highlight box with dynamic snippets (e.g., `StartPollers`, `StartHTTPPollers`, `StartSNMPPollers`).
- Documentation and tools:
  - Added `requirements.txt` with MkDocs dependencies (`mkdocs`, `mkdocs-material`, `mkdocs-static-i18n`, `pymdown-extensions`).
  - Updated instructions in `docs/contribuicao.md` and `docs/contribuicao.en.md` to run the documentation locally and resolve a common TLS error.
  - Included `.gitignore` to ignore `.venv` and temporary files.
  - `README.md` redesigned with Quick Start and development instructions.
  - Update Docs with new format and support multiple languages (Portuguese and English) using `mkdocs-static-i18n`. Contribute via Pull Request with the title `i18n: add <language>` (e.g. `i18n: add es_ES`).

## Affected files

- `app/cmd/app/main.go` — logic for generating the report's HTML (Users tab `tab-usuarios`, rec sections, proxy fixes).
- `app/web/locales/*/messages.json` — new i18n keys for messages and recommendations.
- `docs/*` — updated documentation and auxiliary files (`requirements.txt`, `.gitignore`, README`).

## Notes

- Test `user.login` only in the staging environment before using it in production (account lockout policies may occur).
- To serve the documentation locally, use a `venv` and run `python -m mkdocs serve`.

## [0.0.1] - 15/03/2026
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