package main

import (
	"strings"
	"testing"
)

// Com APP_DEBUG=1 o corpo JSON-RPC inteiro vai para o log. Sem redação isso
// expõe o token de API em toda chamada e a senha do usuário no user.login.
func TestRedactSecrets(t *testing.T) {
	const apiToken = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
	const senha = "SenhaSuperSecreta123"

	cases := []struct {
		name        string
		body        string
		loginResult bool
		leaks       string
	}{
		{
			name:  "token de API no campo auth",
			body:  `{"jsonrpc":"2.0","method":"item.get","params":{"output":"extend"},"auth":"` + apiToken + `","id":1}`,
			leaks: apiToken,
		},
		{
			name:  "senha no user.login",
			body:  `{"jsonrpc":"2.0","method":"user.login","params":{"username":"Admin","password":"` + senha + `"},"id":1}`,
			leaks: senha,
		},
		{
			name:        "token de sessão devolvido pelo user.login",
			body:        `{"jsonrpc":"2.0","result":"` + apiToken + `","id":1}`,
			loginResult: true,
			leaks:       apiToken,
		},
	}

	for _, c := range cases {
		got := redactSecrets(c.body, c.loginResult)
		if strings.Contains(got, c.leaks) {
			t.Errorf("%s: segredo vazou no log: %s", c.name, got)
		}
		if !strings.Contains(got, "<redacted>") {
			t.Errorf("%s: nada foi redigido: %s", c.name, got)
		}
	}
}

// O "result" das demais chamadas são dados do relatório, não credenciais —
// redigir tudo cegamente tornaria o modo debug inútil.
func TestRedactSecretsPreservaDados(t *testing.T) {
	body := `{"jsonrpc":"2.0","result":[{"itemid":"10500","lastvalue":"42"}],"id":1}`
	if got := redactSecrets(body, false); got != body {
		t.Errorf("dados do relatório não devem ser redigidos:\n antes: %s\ndepois: %s", body, got)
	}
	// apiinfo.version devolve string em result, mas não é user.login
	ver := `{"jsonrpc":"2.0","result":"7.0.28","id":1}`
	if got := redactSecrets(ver, false); !strings.Contains(got, "7.0.28") {
		t.Errorf("versão do Zabbix não deve ser redigida: %s", got)
	}
}
