---
title: "Contributing"
lang: en_US
---

# Contributing

Contributions are welcome!

1. Fork the project
2. Create a branch
3. Submit your PR

Suggestions, issues and improvements are appreciated.

---

## Contributing new languages

ZBX-Easy uses a JSON-based i18n system. Adding a new language requires only four steps:

### 1. Copy an existing locale

Create a folder for the new language inside `app/web/locales/` using the standard locale code (`xx_YY`):

```bash
cp -r app/web/locales/pt_BR app/web/locales/es_ES
```

### 2. Translate the keys

Open `app/web/locales/es_ES/messages.json` and translate **only the values** (right-hand side). Keys (left-hand side) must remain unchanged.

!!! warning "Important"
    - Keep `%s` and `%d` placeholders in translations that contain them.
    - Do not change JSON keys.
    - The `pt_BR/messages.json` file (~290 keys) is the most complete reference.

### 3. Register the language in the selector

Edit `app/web/templates/index.html` and add an `<option>` to the `#lang-select` selector:

```html
<select id="lang-select">
  <option value="pt_BR">🇧🇷 PT</option>
  <option value="en_US">🇺🇸 EN</option>
  <option value="es_ES">🇪🇸 ES</option>  <!-- new -->
</select>
```

### 4. Test

```bash
docker compose up -d --build
```

### Tips

- Use the browser console to check untranslated keys — if a key doesn't exist in the dictionary, the original HTML text will be kept.
- Compare your `messages.json` with `pt_BR` to ensure no keys are missing.
- Contribute via Pull Request with the title `i18n: add <language>` (e.g. `i18n: add es_ES`).

---

## Running the docs locally (MkDocs)

To preview the documentation locally we use `mkdocs`. The repository includes a `requirements.txt` with the needed packages.

1. Create and activate a virtual environment (recommended):

```bash
python3 -m venv .venv
source .venv/bin/activate
```

2. Install dependencies:

```bash
pip install -r requirements.txt
```

3. Serve the site locally:

```bash
mkdocs serve
```

Open `http://127.0.0.1:8000` in your browser.

## TLS / certificate error troubleshooting (e.g. "Could not find a suitable TLS CA certificate bundle")

If `pip install -r requirements.txt` fails with an error like:

```
Could not find a suitable TLS CA certificate bundle, invalid path: /path/to/certifi/cacert.pem
```

it's likely an environment variable points to a missing or invalid CA bundle. Fix it as follows:

1. Check TLS-related environment variables:

```bash
echo $SSL_CERT_FILE
echo $REQUESTS_CA_BUNDLE
echo $PIP_CERT
```

2. If any point to an invalid path (e.g. `/path/to/certifi/cacert.pem`), unset them temporarily:

```bash
unset SSL_CERT_FILE REQUESTS_CA_BUNDLE PIP_CERT
```

3. Reinstall/upgrade `certifi` and get its bundle path:

```bash
pip install --upgrade certifi
python -c "import certifi; print(certifi.where())"
```

4. Optionally export that path if your environment requires an explicit variable:

```bash
export SSL_CERT_FILE="$(python -c 'import certifi; print(certifi.where())')"
```

5. Retry installation and serving:

```bash
pip install -r requirements.txt
mkdocs serve
```

If you're behind a corporate proxy using a custom CA, set `SSL_CERT_FILE` to your organization's CA bundle instead of the `certifi` bundle.

