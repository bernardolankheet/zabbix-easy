package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Dois relatórios simultâneos corriam em três globais dentro de zabbixApiRequest:
// httpClient/httpTransport (init preguiçosa sem sincronização) e useBearerAuth.
// Rodar este teste com -race é o que pega a regressão; sem -race ele só confirma
// que as chamadas concorrentes funcionam.
func TestZabbixApiRequestConcorrente(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":[]}`)
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// metade escreve o modo de auth, metade faz requisição — é a mistura
			// que disparava a corrida
			if n%2 == 0 {
				setBearerAuth(srv.URL, n%4 == 0)
			}
			if _, err := zabbixApiRequest(srv.URL, "tok", "item.get", map[string]interface{}{}); err != nil {
				t.Errorf("requisição %d falhou: %v", n, err)
			}
		}(i)
	}
	wg.Wait()
}

// O transporte de autenticação é por URL: dois Zabbix de versões diferentes no
// mesmo processo não podem se sobrescrever. Antes era um bool global — o último
// relatório a detectar a versão decidia por todos.
func TestBearerAuthPorUrl(t *testing.T) {
	var mu sync.Mutex
	authPorUrl := map[string]bool{} // url -> viu header Bearer

	novo := func(bearer bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			authPorUrl["http://"+r.Host] = r.Header.Get("Authorization") != ""
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":[]}`)
		}))
	}
	v72, v70 := novo(true), novo(false)
	defer v72.Close()
	defer v70.Close()

	setBearerAuth(v72.URL, true)  // Zabbix >= 7.2
	setBearerAuth(v70.URL, false) // Zabbix < 7.2

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		for _, u := range []string{v72.URL, v70.URL} {
			wg.Add(1)
			go func(u string) {
				defer wg.Done()
				if _, err := zabbixApiRequest(u, "tok", "item.get", map[string]interface{}{}); err != nil {
					t.Errorf("%s: %v", u, err)
				}
			}(u)
		}
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if !authPorUrl[v72.URL] {
		t.Errorf("Zabbix >= 7.2 (%s) deveria receber Authorization: Bearer", v72.URL)
	}
	if authPorUrl[v70.URL] {
		t.Errorf("Zabbix < 7.2 (%s) não deveria receber Authorization; o token vai no campo auth", v70.URL)
	}
}
