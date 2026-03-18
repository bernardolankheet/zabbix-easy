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
