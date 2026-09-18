package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// trend.get só aceita time_from/time_till. Enviar "time_to" faz a API responder
// -32602 unexpected parameter, o que derruba todo o cálculo de trends para o
// fallback caro de history.get. Este teste trava os nomes dos parâmetros.
func trendParams(t *testing.T, call func(apiUrl string)) map[string]interface{} {
	t.Helper()
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("corpo inválido: %v", err)
		}
		if req.Method == "trend.get" {
			got = req.Params
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":[]}`)
	}))
	defer srv.Close()
	call(srv.URL)
	if got == nil {
		t.Fatal("trend.get não foi chamado")
	}
	return got
}

func assertTrendTimeRange(t *testing.T, p map[string]interface{}) {
	t.Helper()
	if _, bad := p["time_to"]; bad {
		t.Errorf(`trend.get enviou "time_to" — a API do Zabbix rejeita com -32602; use "time_till"`)
	}
	if _, ok := p["time_till"]; !ok {
		t.Errorf(`trend.get sem "time_till" (params: %v)`, p)
	}
	if _, ok := p["time_from"]; !ok {
		t.Errorf(`trend.get sem "time_from" (params: %v)`, p)
	}
}

func TestGetLastTrendTimeRange(t *testing.T) {
	assertTrendTimeRange(t, trendParams(t, func(apiUrl string) {
		if _, err := getLastTrend(apiUrl, "tok", "10500", 30); err != nil {
			t.Fatal(err)
		}
	}))
}

func TestGetTrendsBulkStatsTimeRange(t *testing.T) {
	assertTrendTimeRange(t, trendParams(t, func(apiUrl string) {
		if _, err := getTrendsBulkStats(apiUrl, "tok", []string{"10500", "10501"}); err != nil {
			t.Fatal(err)
		}
	}))
}
