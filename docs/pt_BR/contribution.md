---
title: "Contribuição"
lang: pt_BR
---

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

---

## Executando a documentação localmente (MkDocs)

Para testar o site de documentação localmente usamos `mkdocs`. O projeto inclui um `requirements.txt` com as dependências necessárias.

1. Crie e ative um ambiente virtual (recomendado):

```bash
python3 -m venv .venv
source .venv/bin/activate
```

2. Instale as dependências:

```bash
pip install -r requirements.txt
```

3. Sirva a documentação localmente:

```bash
python -m mkdocs serve
mkdocs serve
```

Abra `http://127.0.0.1:8000` no navegador.

## Solução para erro TLS / certificado (ex: "Could not find a suitable TLS CA certificate bundle")

Se ao instalar as dependências com `pip` você receber um erro semelhante a:

```
Could not find a suitable TLS CA certificate bundle, invalid path: /path/to/certifi/cacert.pem
```

provavelmente há uma variável de ambiente apontando para um bundle de CA inválido ou ausente. Siga estes passos para resolver:

1. Verifique variáveis que afetam TLS:

```bash
echo $SSL_CERT_FILE
echo $REQUESTS_CA_BUNDLE
echo $PIP_CERT
```

2. Se alguma apontar para um caminho inválido (ex.: `/path/to/certifi/cacert.pem`), remova-a temporariamente:

```bash
unset SSL_CERT_FILE REQUESTS_CA_BUNDLE PIP_CERT
```

3. Reinstale/atualize `certifi` e recupere o caminho do bundle confiável:

```bash
pip install --upgrade certifi
python -c "import certifi; print(certifi.where())"
```

4. Opcional: exporte esse caminho se seu ambiente requerer uma variável explícita:

```bash
export SSL_CERT_FILE="$(python -c 'import certifi; print(certifi.where())')"
```

5. Tente novamente:

```bash
pip install -r requirements.txt
mkdocs serve
```

Se você estiver em uma rede corporativa com CA interna, aponte `SSL_CERT_FILE` para o bundle CA da sua organização.