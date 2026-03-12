# Contribuição

Contribuições são bem-vindas!

1. Fork o projeto
2. Crie uma branch
3. Envie seu PR

Sugestões, issues e melhorias são apreciadas.

---

## Contribuindo com novos idiomas

O ZBX-Easy utiliza um sistema de i18n baseado em arquivos JSON. Adicionar um novo idioma requer apenas quatro passos:

### 1. Copiar um locale existente

Crie uma pasta para o novo idioma dentro de `app/web/locales/` usando o código de locale padrão (`xx_YY`):

```bash
cp -r app/web/locales/pt_BR app/web/locales/es_ES
```

### 2. Traduzir as chaves

Abra o arquivo `app/web/locales/es_ES/messages.json` e traduza **apenas os valores** (lado direito). As chaves (lado esquerdo) devem permanecer iguais:

```json
{
  "label_url": "URL del Zabbix",
  "label_token": "Token de API",
  "btn_generate": "Generar informe",
  "tabs.summary": "Resumen",
  ...
}
```

!!! warning "Atenção"
    - Mantenha os marcadores `%s` e `%d` nas traduções que os contêm — eles são substituídos por valores dinâmicos.
    - Não altere as chaves JSON.
    - O arquivo `pt_BR/messages.json` (~290 chaves) é a referência mais completa.

### 3. Registrar o idioma no seletor

Edite `app/web/templates/index.html` e adicione um `<option>` ao seletor `#lang-select`:

```html
<select id="lang-select">
  <option value="pt_BR">🇧🇷 PT</option>
  <option value="en_US">🇺🇸 EN</option>
  <option value="es_ES">🇪🇸 ES</option>  <!-- novo -->
</select>
```

### 4. Testar

Suba o ambiente com Docker Compose para validar:

```bash
docker compose up -d --build
```

Acesse a interface, selecione o novo idioma no seletor do header e verifique se todos os textos estão traduzidos corretamente — tanto na interface quanto no relatório gerado.

### Dicas

- Use o console do navegador para verificar chaves não traduzidas: se uma chave não existir no dicionário, o texto original do HTML será mantido.
- Compare seu `messages.json` com o de `pt_BR` para garantir que nenhuma chave foi omitida.
- Contribua via Pull Request com o título `i18n: add <idioma>` (ex.: `i18n: add es_ES`).
