package collector

import (
	"fmt"
	"testing"
)

func TestAuthenticate(t *testing.T) {
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method != "user.login" {
			t.Fatalf("unexpected method: %s", method)
		}
		if token != "" {
			t.Fatalf("user.login must be sent unauthenticated, got token %q", token)
		}
		p, ok := params.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected params type %T", params)
		}
		// Zabbix >= 6.0 expects "username"/"password" (não "user").
		if p["username"] != "Admin" || p["password"] != "zabbix" {
			t.Fatalf("unexpected params: %v", p)
		}
		return map[string]interface{}{"result": "abc123"}, nil
	}
	tok, err := Authenticate("u", "Admin", "zabbix", fake)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "abc123" {
		t.Fatalf("unexpected token %q", tok)
	}
}

func TestAuthenticateErrors(t *testing.T) {
	cases := map[string]ApiRequester{
		"api error": func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"error": map[string]interface{}{"data": "Incorrect user name or password"}}, nil
		},
		"empty token": func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"result": "  "}, nil
		},
		"transport error": func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
			return nil, fmt.Errorf("connection refused")
		},
		"unexpected shape": func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		},
	}
	for name, fake := range cases {
		if tok, err := Authenticate("u", "Admin", "wrong", fake); err == nil {
			t.Fatalf("%s: expected error, got token %q", name, tok)
		}
	}
}
