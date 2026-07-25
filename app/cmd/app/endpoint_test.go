package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Apontar a ferramenta para uma URL que não é a API do Zabbix (um Grafana, por
// exemplo) fazia a coleta seguir com majorV=0: o transporte de auth errado para
// 7.2+ e um erro confuso lá na frente, em vez de "essa URL não é o Zabbix".
func TestGenerateZabbixReportRejeitaEndpointNaoZabbix(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		// resposta real de um Grafana ao POST em /api_jsonrpc.php
		"grafana 401": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"message":"Unauthorized","messageId":"auth.unauthorized","statusCode":401}`)
		},
		"html qualquer": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `<html><body>File not found.</body></html>`)
		},
		"json sem result": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"jsonrpc":"2.0","id":1}`)
		},
	}

	for name, h := range cases {
		srv := httptest.NewServer(h)
		_, err := generateZabbixReport(srv.URL, "tok", "", "", nil)
		srv.Close()

		if err == nil {
			t.Errorf("%s: esperava erro, coleta seguiu adiante", name)
			continue
		}
		if !strings.HasPrefix(err.Error(), "not a Zabbix API endpoint") {
			t.Errorf("%s: erro deveria identificar a URL como não-Zabbix, veio: %v", name, err)
		}
	}
}

// Um Zabbix de verdade responde apiinfo.version sem autenticação — nesse caso a
// coleta precisa passar da detecção de versão (falha depois, no primeiro
// collector, que este stub não implementa).
func TestGenerateZabbixReportAceitaApiinfoValido(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"jsonrpc":"2.0","result":"7.0.28","id":1}`)
	}))
	defer srv.Close()

	_, err := generateZabbixReport(srv.URL, "tok", "", "", nil)
	if err != nil && strings.HasPrefix(err.Error(), "not a Zabbix API endpoint") {
		t.Fatalf("apiinfo.version válido não pode ser tratado como endpoint inválido: %v", err)
	}
}
