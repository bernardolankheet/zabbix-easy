package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"crypto/tls"
	"fmt"
	"time"
	"strings"
	htmlpkg "html"
	"encoding/json"
	"log"
	"io"
	"strconv"
	"sort"
	neturl "net/url"
	"sync"
	"os"
	"database/sql"
	_ "github.com/lib/pq"
)

// Debug flag controlled by ENV APP_DEBUG (true/1/yes to enable)
var debugApi bool = false
// CHECKTRENDTIME controls how far back getLastTrend queries trends.
// Format examples: 15d, 1d, 12h, 10m (days/hours/minutes). Defaults to 15d.
var checkTrendDurationSeconds int64 = 15 * 24 * 60 * 60

// parseCheckTrendEnv lê a variável de ambiente CHECKTRENDTIME e converte o valor
// para segundos, armazenando em checkTrendDurationSeconds.
//
// Formatos aceitos (case-insensitive):
//
//	"15d"  → 15 dias  (1296000 s)   ← padrão se a variável não estiver definida
//	"12h"  → 12 horas (43200 s)
//	"90m"  → 90 minutos (5400 s)
//	"3600" → sem sufixo = minutos (3600 minutos)
//
// checkTrendDurationSeconds é usado por getLastTrend, getTrendsBulkStats e
// getHistoryStats para determinar o intervalo time_from/time_to nas chamadas
// à API do Zabbix.
//
// ─── Como alterar o padrão ────────────────────────────────────────────────
// O valor padrão (15d) é definido na declaração de checkTrendDurationSeconds
// no topo do arquivo. Altere lá caso queira um padrão diferente sem usar ENV.
func parseCheckTrendEnv() {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("CHECKTRENDTIME")))
	if v == "" { v = "15d" }
	// last char is unit
	n := len(v)
	unit := v[n-1]
	numPart := v[:n-1]
	// if unit is digit (no unit supplied), treat whole as minutes
	       if unit >= '0' && unit <= '9' {
		       // entire string is numeric -> minutes
		       if val, err := strconv.Atoi(v); err == nil {
			       checkTrendDurationSeconds = int64(val) * 60
			       log.Printf("[DEBUG] CHECKTRENDTIME parsed as %d minutes", val)
			       return
		       }
		       log.Printf("[WARN] CHECKTRENDTIME invalid: %s", v)
		       return
	       }
	val, err := strconv.Atoi(numPart)
	if err != nil {
		log.Printf("[WARN] CHECKTRENDTIME invalid number: %s", v)
		return
	}
	       switch unit {
	       case 'd':
		       checkTrendDurationSeconds = int64(val) * 24 * 60 * 60
	       case 'h':
		       checkTrendDurationSeconds = int64(val) * 60 * 60
	       case 'm':
		       checkTrendDurationSeconds = int64(val) * 60
	       default:
		       log.Printf("[WARN] CHECKTRENDTIME invalid unit '%c' in %s", unit, v)
		       return
	       }
	log.Printf("[DEBUG] CHECKTRENDTIME set to %s -> %d seconds", v, checkTrendDurationSeconds)

}

// Reusable HTTP client to improve performance (connection reuse)
var httpClient *http.Client
var httpTransport *http.Transport

// Simple cache for item lookups: key is key+"|"+hostid -> map[string]interface{}
var itemLookupCache sync.Map

// Optional progress callback used to send textual progress updates back to caller
var progressCb func(string)

func initHttpClient() {
	if httpClient != nil {
		return
	}
	httpTransport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	}
	httpClient = &http.Client{Transport: httpTransport, Timeout: 20 * time.Second}
}

// isIdleConnError detecta erros transientes de conexão idle que ocorrem quando
// o servidor fecha uma conexão keep-alive antes que o cliente tente reutilizá-la.
// Nesse caso é seguro retentar a mesma requisição com uma conexão nova.
func isIdleConnError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "server closed idle connection") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "broken pipe")
}

// zabbixApiRequest é o ponto central de comunicação com a API JSON-RPC do Zabbix.
// Toda chamada à API (item.get, trend.get, host.get, etc.) passa por aqui.
//
// Parâmetros:
//
//	apiUrl  — URL completa do endpoint, ex: "https://zabbix.dominio.com/api_jsonrpc.php"
//	token   — token de autenticação (campo "auth" no JSON-RPC). Passe "" para
//	           chamadas que não requerem autenticação, como apiinfo.version.
//	method  — método da API Zabbix, ex: "item.get", "trend.get", "host.get"
//	params  — qualquer struct/map que será serializado como o campo "params" do JSON-RPC
//
// Retorno:
//
//	map[string]interface{} com a resposta completa do JSON-RPC (inclui "result").
//	Erro se a requisição HTTP falhar, se o JSON não puder ser parseado, ou se
//	a resposta contiver o campo "error" (erro da própria API Zabbix).
//
// Observações:
//
//	• Usa httpClient global (reutilização de conexão, TLS sem verificação).
//	• Se APP_DEBUG=1, loga o request e os primeiros 4096 bytes da resposta.
//	• Sempre loga o tempo de execução de cada chamada.
func zabbixApiRequest(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	if token != "" {
		req["auth"] = token
	}
	reqBytes, _ := json.Marshal(req)
	if debugApi {
		log.Printf("[ZABBIX DEBUG] Request %s -> %s", method, string(reqBytes))
	}
	if httpClient == nil {
		initHttpClient()
	}
	// Função para tentar uma requisição e repetir se for um erro de idle connection (conexão fechada pelo servidor antes de concluirr a requisição). Limite de 3 tentativas para evitar loops infinitos.
	const maxRetries = 3
	var resp *http.Response
	var err error
	start := time.Now()
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Para forçar uma nova conexão TCP no retry (sem afetar as conexões das outras goroutines),
		// adicionamos "Connection: close" no retry — o servidor fecha após responder e o Go não
		// recoloca a conexão no pool, evitando reusar stale connections sem CloseIdleConnections() global.
		req, reqErr := http.NewRequest("POST", apiUrl, strings.NewReader(string(reqBytes)))
		if reqErr != nil { err = reqErr; break }
		req.Header.Set("Content-Type", "application/json")
		if attempt > 1 {
			req.Header.Set("Connection", "close")
		}
		resp, err = httpClient.Do(req)
		if err == nil {
			break // sucesso
		}
		if isIdleConnError(err) && attempt < maxRetries {
			log.Printf("[ZABBIX API] %s idle-conn error (tentativa %d/%d), repetindo: %v", method, attempt, maxRetries, err)
			time.Sleep(time.Duration(attempt*100) * time.Millisecond)
			continue
		}
		break
	}
	if err != nil {
		log.Printf("[ZABBIX API] %s failed after %s: %v", method, time.Since(start), err)
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if debugApi {
		b := string(bodyBytes)
		if len(b) > 4096 { b = b[:4096] + "...(truncated)" }
		log.Printf("[ZABBIX DEBUG] Response %s <- status=%s body=%s", method, resp.Status, b)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		log.Printf("[ZABBIX API] %s unmarshal failed after %s: %v", method, time.Since(start), err)
		return nil, err
	}
	if errObj, ok := result["error"]; ok {
		log.Printf("[ZABBIX API] %s returned error after %s: %v", method, time.Since(start), errObj)
		return nil, fmt.Errorf("API error: %v", errObj)
	}
	log.Printf("[ZABBIX API] %s completed in %s, status=%s", method, time.Since(start), resp.Status)
	return result, nil
}

// getItemByKey busca um item específico pela chave exata (key_) e, opcionalmente,
// filtra pelo hostid. Retorna o primeiro resultado ou nil se não encontrado.
//
// Uso típico:
//
//	item, err := getItemByKey(apiUrl, token, "zabbix[requiredperformance]", hostid)
//	if item != nil {
//	    itemid := fmt.Sprintf("%v", item["itemid"])
//	}
//
// Resultado é cacheado em itemLookupCache (sync.Map) usando "key|hostid" como
// chave do cache — chamadas subsequentes com os mesmos parâmetros não fazem
// nova requisição à API.
//
// ─── Diferença em relação a getProcessItemsBulk ───────────────────────────
// Esta função usa filter exato (key_ == valor). Use-a para chaves conhecidas
// e fixas (ex: "zabbix[requiredperformance]").
// Use getProcessItemsBulk quando quiser buscar múltiplos processos de uma vez
// com padrões wildcard.
func getItemByKey(apiUrl, token, key, hostid string) (map[string]interface{}, error) {
	// check cache first
	cacheKey := key + "|" + hostid
	if v, ok := itemLookupCache.Load(cacheKey); ok {
		if m, ok2 := v.(map[string]interface{}); ok2 {
			return m, nil
		}
	}

	params := map[string]interface{}{
		"output": []string{"itemid", "hostid", "name", "key_", "value_type"},
		// use exact filter for key_ to avoid partial matches and extra work
		"filter": map[string]interface{}{"key_": key},
		"limit": 1,
	}
	if hostid != "" {
		params["hostids"] = hostid
	}
	resp, err := zabbixApiRequest(apiUrl, token, "item.get", params)
	if err != nil { return nil, err }
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		if len(arr) > 0 {
			m := arr[0].(map[string]interface{})
			itemLookupCache.Store(cacheKey, m)
			return m, nil
		}
	}
	return nil, nil
}

// nameToWildcard converte um nome legível de processo em um padrão wildcard
// compatível com o campo search.key_ do item.get (searchWildcardsEnabled=true).
//
// Exemplos:
//
//	"agent poller"   → "*agent*poller*"
//	"http poller"    → "*http*poller*"
//	"data*sender"    → "*data*sender*"  ("*" no meio já funciona como separador)
//
// O padrão gerado casa a chave do item independentemente do separador usado
// pela versão do Zabbix (espaço, underscore, ponto, etc.). Por exemplo,
// "*agent*poller*" bate tanto em:
//
//	"zabbix[process,agent poller,avg,busy]"   (Zabbix 6)
//	"zabbix[process,agent_poller,avg,busy]"   (Zabbix 7+)
//
// ─── Quando modificar ─────────────────────────────────────────────────────
// Raramente necessário. Se o Zabbix mudar o formato das chaves de forma
// incompatível, ajuste a lógica de separação aqui. Para adicionar processos
// novos, basta incluir o nome nas listas (pollerNames, procNames, etc.).
func nameToWildcard(name string) string {
	words := strings.Fields(strings.TrimSpace(name))
	return "*" + strings.Join(words, "*") + "*"
}

// wildcardMatch verifica se a string s casa com o padrão wildcard (apenas "*" suportado).
// A comparação é case-insensitive.
//
// Exemplos:
//
//	wildcardMatch("*agent*poller*", "zabbix[process,agent poller,avg,busy]")  → true
//	wildcardMatch("*poller*",       "zabbix[process,agent poller,avg,busy]")  → true
//	wildcardMatch("*poller*",       "zabbix[history_syncer]")                 → false
//
// Usado em getProcessItemsBulk e getProxyProcessItems para mapear os itens
// retornados pela API de volta ao nome de processo original.
// A prioridade "mais específico primeiro" (mais palavras no padrão) é garantida
// pelo sort feito antes de chamar esta função nos callers.
//
// ─── Quando modificar ─────────────────────────────────────────────────────
// Apenas se precisar suportar wildcards adicionais (ex: "?"). Atualmente
// somente "*" é reconhecido.
func wildcardMatch(pattern, s string) bool {
	parts := strings.Split(strings.ToLower(pattern), "*")
	sl := strings.ToLower(s)
	pos := 0
	for _, p := range parts {
		if p == "" { continue }
		idx := strings.Index(sl[pos:], p)
		if idx < 0 { return false }
		pos += idx + len(p)
	}
	return true
}

// getProcessItemsBulk busca itens de processo para o Zabbix Server em UMA única
// chamada item.get usando wildcard (searchWildcardsEnabled + searchByAny).
//
// Retorna map[nomeEmMinúsculas] → item (os campos itemid, key_, name, value_type).
//
// Estratégia de resolução de conflitos ("mais específico vence"):
//
//	Se dois padrões batem no mesmo item (ex: "*poller*" e "*agent*poller*"),
//	o padrão com mais palavras tem prioridade. Isso evita que o padrão genérico
//	"*poller*" roube o item do "*agent*poller*".
//
// Parâmetros:
//
//	names  — lista de nomes de processo (ex: ["agent poller", "history syncer"])
//	hostid — se não vazio, filtra pelo host (use ZABBIX_SERVER_HOSTID)
//
// ─── Diferença em relação a getProxyProcessItems ──────────────────────────
// Esta função usa search.key_ wildcard: funciona bem quando as chaves seguem
// um padrão previsível. Para proxies, prefira getProxyProcessItems, que busca
// todos os itens type=5 e faz o match client-side — mais robusto a variações
// de formato de chave entre versões do Zabbix.
//
// ─── Como adicionar um processo novo ─────────────────────────────────────
// Inclua o nome em pollerNames ou procNames dentro de generateZabbixReport.
// Esta função é chamada automaticamente com a lista completa.
func getProcessItemsBulk(apiUrl, token string, names []string, hostid string) (map[string]map[string]interface{}, error) {
	if len(names) == 0 { return map[string]map[string]interface{}{}, nil }
	patterns := make([]string, len(names))
	for i, n := range names { patterns[i] = nameToWildcard(n) }
	params := map[string]interface{}{
		"output":                 []string{"itemid", "hostid", "name", "key_", "value_type"},
		"search":                 map[string]interface{}{"key_": patterns},
		"searchByAny":            true,
		"searchWildcardsEnabled": true,
	}
	if hostid != "" { params["hostids"] = hostid }
	resp, err := zabbixApiRequest(apiUrl, token, "item.get", params)
	if err != nil { return nil, err }
	// Sort entries by word count desc so most-specific pattern is tried first
	type nameEntry struct{ norm string; words int; pattern string }
	entries := make([]nameEntry, len(names))
	for i, n := range names {
		entries[i] = nameEntry{norm: strings.ToLower(strings.TrimSpace(n)), words: len(strings.Fields(n)), pattern: patterns[i]}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].words > entries[j].words })
	result := map[string]map[string]interface{}{}
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		for _, raw := range arr {
			item, _ := raw.(map[string]interface{})
			if item == nil { continue }
			itemKey := fmt.Sprintf("%v", item["key_"])
			for _, e := range entries {
				if wildcardMatch(e.pattern, itemKey) {
					if _, exists := result[e.norm]; !exists { result[e.norm] = item }
					break
				}
			}
		}
	}
	return result, nil
}

// getProxyProcessItems busca itens de monitoramento de processos para um único
// host de proxy em UMA chamada item.get, filtrando por type=5 (Zabbix internal).
//
// Estratégia (mais robusta que wildcard na key_):
//
//	1. Busca TODOS os itens do tipo 5 (Zabbix internal) do host de uma vez.
//	2. Faz o match client-side usando nameToWildcard, testando TANTO key_ QUANTO
//	   o campo name do item.
//
// Por que checar o campo name?
//
//	O Zabbix 7 alterou o formato da key_ de alguns processos do proxy.
//	O campo name (ex: "Utilization of data sender processes, in %") é estável
//	entre versões. Ao checar os dois campos, a função funciona em Zabbix 6 e 7
//	sem ajustes.
//
// Parâmetros:
//
//	names  — lista de nomes (ex: ["data*sender", "poller"]) de proxyAllProcNames
//	hostid — hostid do host de auto-monitoramento do proxy (pode diferir do proxyid
//	         no Zabbix 7; veja o Step 1 do goroutine de proxy em generateZabbixReport)
//
// Retorna map[nomeEmMinúsculas] → item.
//
// ─── Como adicionar um processo novo ao proxy ─────────────────────────────
// Inclua o nome em proxyAllProcNames dentro de generateZabbixReport.
// Use "*" como separador de palavras para que nameToWildcard gere o padrão
// correto. Ex: "nova*feature" → padrão "*nova*feature*".
func getProxyProcessItems(apiUrl, token string, names []string, hostid string) (map[string]map[string]interface{}, error) {
	if len(names) == 0 || hostid == "" { return map[string]map[string]interface{}{}, nil }
	params := map[string]interface{}{
		"output":  []string{"itemid", "hostid", "name", "key_", "value_type"},
		"hostids": hostid,
		"filter":  map[string]interface{}{"type": 5}, // Zabbix internal items only
	}
	resp, err := zabbixApiRequest(apiUrl, token, "item.get", params)
	if err != nil { return nil, err }
	// Build name entries sorted by word count desc (most specific pattern wins)
	type nameEntry struct{ norm string; words int; pattern string }
	entries := make([]nameEntry, len(names))
	for i, n := range names {
		entries[i] = nameEntry{
			norm:    strings.ToLower(strings.TrimSpace(n)),
			words:   len(strings.Fields(n)),
			pattern: nameToWildcard(n),
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].words > entries[j].words })
	result := map[string]map[string]interface{}{}
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		if debugApi { log.Printf("[DEBUG] getProxyProcessItems hostid=%s: %d type=5 items returned", hostid, len(arr)) }
		for _, raw := range arr {
			item, _ := raw.(map[string]interface{})
			if item == nil { continue }
			itemKey  := fmt.Sprintf("%v", item["key_"])
			itemName := fmt.Sprintf("%v", item["name"])
			for _, e := range entries {
				// Match against key_ OR item name — covers both old and new key formats
				if wildcardMatch(e.pattern, itemKey) || wildcardMatch(e.pattern, itemName) {
					if _, exists := result[e.norm]; !exists { result[e.norm] = item }
					break
				}
			}
		}
	}
	return result, nil
}

// getLastHistoryValue retorna o valor mais recente do histórico de um item
// (history.get com sortorder DESC, limit 1).
//
// Parâmetros:
//
//	itemid      — ID do item no Zabbix
//	historyType — tipo de histórico (value_type do item):
//	               0 = float, 1 = char/string, 2 = log, 3 = integer, 4 = text
//
// Retorna o valor como string (ex: "3.14", "200") ou "" se não houver dados.
//
// Uso típico: obter o último valor de NVPS (zabbix[requiredperformance]).
//
// ─── Diferença em relação a getHistoryStats ───────────────────────────────
// Esta função retorna apenas o último ponto (limit:1). Use getHistoryStats
// quando precisar de min/avg/max em um intervalo de tempo.
func getLastHistoryValue(apiUrl, token, itemid string, historyType int) (string, error) {
	params := map[string]interface{}{
		"output": "extend",
		"history": historyType,
		"itemids": itemid,
		"sortfield": "clock",
		"sortorder": "DESC",
		"limit": 1,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "history.get", params)
	if err != nil { return "", err }
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		if len(arr) > 0 {
			hist := arr[0].(map[string]interface{})
			return fmt.Sprintf("%v", hist["value"]), nil
		}
	}
	return "", nil
}

// getHistoryStats é o fallback de trend para UM único item: busca até 2000 pontos
// do histórico no período configurado (CHECKTRENDTIME) e calcula min/avg/max.
//
// Quando usar:
//
//	• Quando getLastTrend retorna nil (item com trends=0, período curto ou
//	  retenção de trend expirada).
//	• Para itens do Zabbix Server (processados individualmente em goroutines).
//
// Parâmetros:
//
//	itemid  — ID do item
//	hisType — tipo de histórico (0=float, 3=int, …)
//	days    — fallback de intervalo em dias SE checkTrendDurationSeconds == 0
//	          (normalmente checkTrendDurationSeconds > 0 e days é ignorado)
//
// Retorna map com "value_min", "value_avg", "value_max" como strings float,
// ou nil se não houver dados.
//
// ─── Diferença em relação a getHistoryStatsBulkByType ────────────────────
// Esta função processa UM item por chamada à API. Para proxies (múltiplos
// itens de uma vez), use getHistoryStatsBulkByType.
func getHistoryStats(apiUrl, token, itemid string, histType int, days int) (map[string]interface{}, error) {
	now := time.Now().Unix()
	var from int64
	if checkTrendDurationSeconds > 0 {
		from = now - checkTrendDurationSeconds
	} else {
		from = now - int64(days*24*60*60)
	}
	params := map[string]interface{}{
		"output":    []string{"value"},
		"history":   histType,
		"itemids":   []string{itemid},
		"time_from": from,
		"time_till": now, // history.get usa time_till (não time_to) no Zabbix 6 e 7
		"sortfield": "clock",
		"sortorder": "ASC",
		"limit":     2000,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "history.get", params)
	if err != nil { return nil, err }
	r, ok := resp["result"]
	if !ok { return nil, nil }
	arr, ok := r.([]interface{})
	if !ok || len(arr) == 0 { return nil, nil }
	var vals []float64
	for _, entry := range arr {
		m, ok := entry.(map[string]interface{})
		if !ok { continue }
		if v, ok := m["value"]; ok {
			if f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); err == nil {
				vals = append(vals, f)
			}
		}
	}
	if len(vals) == 0 { return nil, nil }
	vmin, vmax := vals[0], vals[0]
	sum := 0.0
	for _, v := range vals {
		if v < vmin { vmin = v }
		if v > vmax { vmax = v }
		sum += v
	}
	vavg := sum / float64(len(vals))
	return map[string]interface{}{
		"value_min": fmt.Sprintf("%f", vmin),
		"value_avg": fmt.Sprintf("%f", vavg),
		"value_max": fmt.Sprintf("%f", vmax),
	}, nil
}

// getLastTrend busca o registro mais recente de trend para UM único item dentro
// do período configurado (CHECKTRENDTIME) usando trend.get.
//
// Retorna map com "value_min", "value_avg", "value_max" (strings float)
// ou nil se não houver trend disponível (→ usar getHistoryStats como fallback).
//
// Parâmetros:
//
//	itemid — ID do item
//	days   — fallback de intervalo em dias SE checkTrendDurationSeconds == 0
//	         (normalmente ignorado pois checkTrendDurationSeconds é preenchido
//	          por parseCheckTrendEnv ao iniciar o servidor)
//
// Usado pelos goroutines de pollers e processos do Zabbix Server.
// Para processos de proxy (múltiplos itens de uma vez), use getTrendsBulkStats.
//
// ─── Fluxo recomendado ────────────────────────────────────────────────────
//
//	trendData, _ := getLastTrend(apiUrl, token, itemid, 30)
//	if trendData == nil {
//	    trendData, _ = getHistoryStats(apiUrl, token, itemid, histType, 30)
//	}
func getLastTrend(apiUrl, token, itemid string, days int) (map[string]interface{}, error) {
	now := time.Now().Unix()
	// compute 'from' based on CHECKTRENDTIME if provided, otherwise use days param (in days)
	var from int64
	if checkTrendDurationSeconds > 0 {
		from = now - checkTrendDurationSeconds
	} else {
		from = now - int64(days*24*60*60)
	}
	params := map[string]interface{}{
		"output": []string{"itemid", "clock", "value_min", "value_avg", "value_max"},
		"itemids": []string{itemid},
		"limit": 1,
		"time_from": from,
		"time_to": now,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "trend.get", params)
	if err != nil { return nil, err }
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		if len(arr) > 0 {
			return arr[0].(map[string]interface{}), nil
		}
	}
	return nil, nil
}

// getProxyCount retorna o número total de proxies configurados no Zabbix
// usando proxy.get com countOutput:true.
//
// Usado apenas no card de resumo ("Número de Proxys"). Para obter a lista
// completa de proxies com detalhes, use getProxies.
func getProxyCount(apiUrl, token string) (int, error) {
	params := map[string]interface{}{
		"output": "extend",
		"countOutput": true,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "proxy.get", params)
	if err != nil { return 0, err }
	if r, ok := resp["result"]; ok {
		switch v := r.(type) {
		case float64:
			return int(v), nil
		case int:
			return v, nil
		default:
			if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { return v2, nil }
		}
	}
	return 0, nil
}

// getProcessItemsBulkByHostids busca itens de processo para MÚLTIPLOS hosts em
// UMA única chamada item.get, usando wildcard na key_ (mesma estratégia de
// getProcessItemsBulk) e agrupando os resultados por hostid.
//
// Retorna map[hostid] → map[nomeEmMinúsculas] → item.
//
// ─── Estado atual ─────────────────────────────────────────────────────────
// Função disponível mas não utilizada diretamente pelos proxies no momento
// (substituída por getProxyProcessItems, que usa type=5 + match client-side).
// Mantida para uso futuro ou em cenários onde a abordagem wildcard seja
// preferível (ex: buscar itens de vários hosts de uma vez de forma eficiente).
//
// ─── Quando usar ──────────────────────────────────────────────────────────
// Prefira esta função quando:
//   - Os hostids de todos os proxies/hosts já forem conhecidos antecipadamente.
//   - O formato das chaves for previsível (wildcard confiável).
// Prefira getProxyProcessItems quando o formato da key_ for incerto entre
// versões do Zabbix.
func getProcessItemsBulkByHostids(apiUrl, token string, names []string, hostids []string) (map[string]map[string]map[string]interface{}, error) {
	if len(names) == 0 || len(hostids) == 0 {
		return map[string]map[string]map[string]interface{}{}, nil
	}
	patterns := make([]string, len(names))
	for i, n := range names { patterns[i] = nameToWildcard(n) }
	params := map[string]interface{}{
		"output":                 []string{"itemid", "hostid", "name", "key_", "value_type"},
		"hostids":                hostids,
		"search":                 map[string]interface{}{"key_": patterns},
		"searchByAny":            true,
		"searchWildcardsEnabled": true,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "item.get", params)
	if err != nil { return nil, err }
	// Sort entries by word count desc — most specific pattern wins
	type nameEntry struct{ norm string; words int; pattern string }
	entries := make([]nameEntry, len(names))
	for i, n := range names {
		entries[i] = nameEntry{norm: strings.ToLower(strings.TrimSpace(n)), words: len(strings.Fields(n)), pattern: patterns[i]}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].words > entries[j].words })
	result := map[string]map[string]map[string]interface{}{}
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		for _, raw := range arr {
			item, _ := raw.(map[string]interface{})
			if item == nil { continue }
			hid := fmt.Sprintf("%v", item["hostid"])
			itemKey := fmt.Sprintf("%v", item["key_"])
			for _, e := range entries {
				if wildcardMatch(e.pattern, itemKey) {
					if result[hid] == nil { result[hid] = map[string]map[string]interface{}{} }
					if _, exists := result[hid][e.norm]; !exists { result[hid][e.norm] = item }
					break
				}
			}
		}
	}
	return result, nil
}

// getTrendsBulkStats busca dados de trend para TODOS os itemids em UMA única
// chamada trend.get e agrega os resultados por item.
//
// Agregação por item (quando há múltiplos registros de trend no período):
//
//	value_min → menor de todos os value_min
//	value_avg → média de todos os value_avg
//	value_max → maior de todos os value_max
//
// Retorna map[itemid] → {"value_min", "value_avg", "value_max"} como strings.
// Items sem dados no período não aparecem no mapa (use esse ausência como
// sinal para acionar o fallback getHistoryStatsBulkByType).
//
// O intervalo de tempo é controlado por checkTrendDurationSeconds (CHECKTRENDTIME).
//
// ─── Fluxo recomendado para proxies ───────────────────────────────────────
//
//	trendMap, _ := getTrendsBulkStats(apiUrl, token, iids)
//	// Para itens sem trend, usar fallback de history:
//	missing := map[string]int{}
//	for _, iid := range iids {
//	    if _, ok := trendMap[iid]; !ok { missing[iid] = vtypes[iid] }
//	}
//	if len(missing) > 0 {
//	    histStats, _ := getHistoryStatsBulkByType(apiUrl, token, missing)
//	    for iid, s := range histStats { trendMap[iid] = s }
//	}
func getTrendsBulkStats(apiUrl, token string, itemids []string) (map[string]map[string]interface{}, error) {
	if len(itemids) == 0 { return map[string]map[string]interface{}{}, nil }
	now := time.Now().Unix()
	var from int64
	if checkTrendDurationSeconds > 0 {
		from = now - checkTrendDurationSeconds
	} else {
		from = now - 30*24*60*60
	}
	params := map[string]interface{}{
		"output":    []string{"itemid", "value_min", "value_avg", "value_max"},
		"itemids":   itemids,
		"time_from": from,
		"time_to":   now,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "trend.get", params)
	if err != nil { return nil, err }
	type aggState struct {
		vmin, vmaxV float64
		vavgSum     float64
		count       int
	}
	agg := map[string]*aggState{}
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		for _, raw := range arr {
			row, _ := raw.(map[string]interface{})
			if row == nil { continue }
			iid := fmt.Sprintf("%v", row["itemid"])
			parseF := func(k string) (float64, bool) {
				if v, ok2 := row[k]; ok2 {
					if f, e := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); e == nil { return f, true }
				}
				return 0, false
			}
			vmin, ok1 := parseF("value_min")
			vavg, ok2 := parseF("value_avg")
			vmax, ok3 := parseF("value_max")
			if !ok1 && !ok2 && !ok3 { continue }
			if agg[iid] == nil {
				agg[iid] = &aggState{vmin: vmin, vmaxV: vmax, vavgSum: vavg, count: 1}
			} else {
				s := agg[iid]
				if ok1 && vmin < s.vmin { s.vmin = vmin }
				if ok3 && vmax > s.vmaxV { s.vmaxV = vmax }
				if ok2 { s.vavgSum += vavg; s.count++ }
			}
		}
	}
	result := map[string]map[string]interface{}{}
	for iid, s := range agg {
		vavg := 0.0
		if s.count > 0 { vavg = s.vavgSum / float64(s.count) }
		result[iid] = map[string]interface{}{
			"value_min": fmt.Sprintf("%f", s.vmin),
			"value_avg": fmt.Sprintf("%f", vavg),
			"value_max": fmt.Sprintf("%f", s.vmaxV),
		}
	}
	return result, nil
}

// getHistoryStatsBulkByType é o fallback bulk de trend para MÚLTIPLOS itens.
// Busca histórico agrupando os itemids por value_type e fazendo UMA chamada
// history.get por tipo. Calcula min/avg/max a partir dos valores brutos.
//
// Parâmetros:
//
//	items — map[itemid] → value_type (0=float, 3=int, 1=char, …)
//	         Inclua apenas os itens que não tiveram dados em getTrendsBulkStats.
//
// Limite de segurança: 500 pontos por item, máximo de 20.000 linhas por chamada
// (para evitar respostas gigantes que sobrecarreguem a API ou a memória).
//
// Retorna map[itemid] → {"value_min", "value_avg", "value_max"} como strings.
//
// ─── Quando usar ──────────────────────────────────────────────────────────
// Somente como fallback após getTrendsBulkStats, para os itens que não
// retornaram dados de trend (trends=0 no item, período muito curto, etc.).
// Para um único item (Zabbix Server), use getHistoryStats.
func getHistoryStatsBulkByType(apiUrl, token string, items map[string]int) (map[string]map[string]interface{}, error) {
	if len(items) == 0 { return map[string]map[string]interface{}{}, nil }
	// Group itemids by value_type
	byType := map[int][]string{}
	for iid, vt := range items { byType[vt] = append(byType[vt], iid) }
	now := time.Now().Unix()
	var from int64
	if checkTrendDurationSeconds > 0 {
		from = now - checkTrendDurationSeconds
	} else {
		from = now - 30*24*60*60
	}
	result := map[string]map[string]interface{}{}
	var mu sync.Mutex
	for histType, iids := range byType {
		// Cap limit to avoid huge responses: 500 rows per item
		limit := len(iids) * 500
		if limit > 20000 { limit = 20000 }
		params := map[string]interface{}{
			"output":    []string{"itemid", "value"},
			"history":   histType,
			"itemids":   iids,
			"time_from": from,
			"time_till": now, // history.get usa time_till (não time_to) no Zabbix 6 e 7
			"sortfield": "clock",
			"sortorder": "ASC",
			"limit":     limit,
		}
		resp, err := zabbixApiRequest(apiUrl, token, "history.get", params)
		if err != nil { continue }
		type aggS struct{ vals []float64 }
		agg := map[string]*aggS{}
		if r, ok := resp["result"]; ok {
			arr, _ := r.([]interface{})
			for _, raw := range arr {
				row, _ := raw.(map[string]interface{})
				if row == nil { continue }
				iid := fmt.Sprintf("%v", row["itemid"])
				if f, e := strconv.ParseFloat(fmt.Sprintf("%v", row["value"]), 64); e == nil {
					if agg[iid] == nil { agg[iid] = &aggS{} }
					agg[iid].vals = append(agg[iid].vals, f)
				}
			}
		}
		mu.Lock()
		for iid, s := range agg {
			if len(s.vals) == 0 { continue }
			vmin, vmax, sum := s.vals[0], s.vals[0], 0.0
			for _, v := range s.vals {
				if v < vmin { vmin = v }
				if v > vmax { vmax = v }
				sum += v
			}
			result[iid] = map[string]interface{}{
				"value_min": fmt.Sprintf("%f", vmin),
				"value_avg": fmt.Sprintf("%f", sum/float64(len(s.vals))),
				"value_max": fmt.Sprintf("%f", vmax),
			}
		}
		mu.Unlock()
	}
	return result, nil
}

// getProxies retorna a lista completa de proxies configurados no Zabbix
// com todos os campos disponíveis (output: extend).
//
// Os campos relevantes retornados (variam entre Zabbix 6 e 7):
//
//	Zabbix 6:
//	  proxyid, host (nome), status (5=active, 6=passive), state (0=unknown, 1=offline, 2=online)
//	Zabbix 7:
//	  proxyid, name (nome), operating_mode (0=active, 1=passive), state (0=unknown, 1=offline, 2=online)
//
// A lista é usada em duas partes do relatório:
//
//	1. Tabela de resumo de proxies (status, tipo, fila, itens não suportados)
//	2. Seção "Processos e Threads Zabbix Proxys" (goroutines por proxy)
//
// ─── Compatibilidade Zabbix 6 vs 7 ───────────────────────────────────────
// O código em generateZabbixReport verifica os campos "operating_mode" (v7)
// e "status" (v6) para determinar o tipo (Active/Passive), e "state" para
// determinar o estado (Online/Offline/Unknown).
func getProxies(apiUrl, token string) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output": "extend",
	}
	resp, err := zabbixApiRequest(apiUrl, token, "proxy.get", params)
	if err != nil { return nil, err }
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		out := []map[string]interface{}{}
		for _, it := range arr {
			if m, ok := it.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out, nil
	}
	return nil, nil
}

func generateZabbixReport(url, token string) (string, error) {
		nItemsNaoSuportados := "-"
	log.Printf("[DEBUG] Iniciando coleta Zabbix: url=%s", url)
	apiUrl := url
	// compute frontend base URL (ambienteUrl) early so links can be built
	ambienteUrl := url
	if strings.HasSuffix(ambienteUrl, "/api_jsonrpc.php") {
		ambienteUrl = ambienteUrl[:len(ambienteUrl)-len("/api_jsonrpc.php")]
	}
	// Concurrency limit for parallel API calls (can be configured with env MAX_CCONCURRENT)
	maxConcurrent := 6
	if v := os.Getenv("MAX_CCONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 { maxConcurrent = n }
	}
	log.Printf("[DEBUG] MAX_CCONCURRENT=%d", maxConcurrent)
	// semaphore channel used to bound concurrent API requests across sections
	sem := make(chan struct{}, maxConcurrent)
	if apiUrl[len(apiUrl)-1] != '/' {
		apiUrl += "/api_jsonrpc.php"
	} else {
		apiUrl += "api_jsonrpc.php"
	}

	// get Zabbix API version (apiinfo.version)
	zabbixVersion := ""
	verResp, err := zabbixApiRequest(apiUrl, "", "apiinfo.version", []interface{}{})
	if err == nil {
		if r, ok := verResp["result"]; ok {
			zabbixVersion = fmt.Sprintf("%v", r)
		}
	}
	// Detecta versão do zabbix para ajustar chamadas, funcão para chamadas zabbix 6 e 7, foi uma forma que pensei para ter suporte a ambas.
	majorV := 0
	minorV := 0
	if zabbixVersion != "" {
		parts := strings.SplitN(zabbixVersion, ".", 3)
		if len(parts) > 0 {
			if v, err := strconv.Atoi(parts[0]); err == nil { majorV = v }
		}
		if len(parts) > 1 {
			if v, err := strconv.Atoi(parts[1]); err == nil { minorV = v }
		}
	}
	// minorV may be used elsewhere; avoid unused variable compile error
	_ = minorV
	// get em Consulta quantidade de itens não suportados
	itensNaoSuportadosResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"output": "extend",
		"filter": map[string]interface{}{ "state": 1, "status": 0 },
		"monitored": true,
		"countOutput": true,
	})
	if err == nil {
		nItemsNaoSuportados = fmt.Sprintf("%v", itensNaoSuportadosResp["result"])
	}
	// get Usuários
	if progressCb != nil { progressCb("Coletando informações de Usuários...") }
	userResp, err := zabbixApiRequest(apiUrl, token, "user.get", map[string]interface{}{ "output": "userid" })
	if err != nil { return "", err }
	users := userResp["result"].([]interface{})
	nUsers := len(users)

	// get NVPS (Required server performance, new values per second) utilizando a chave zabbix[requiredperformance] 
	if progressCb != nil { progressCb("Coletando informações de NVPS (Required performance)...") }
	// Flow: item.get with hostid + key_ -> if exists, history.get(last) using item's value_type
	nvps := "N/A"
	requiredHost := os.Getenv("ZABBIX_SERVER_HOSTID")
	if requiredHost == "" { requiredHost = "10084" }
	log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID=%s will be used for item.get", requiredHost)
	if item, err := getItemByKey(apiUrl, token, "zabbix[requiredperformance]", requiredHost); err == nil {
		if item != nil {
			// log item details for debugging
			log.Printf("[DEBUG] Found requiredperformance item: itemid=%v hostid=%v value_type=%v key=%v", item["itemid"], item["hostid"], item["value_type"], item["key_"])
			itemid := fmt.Sprintf("%v", item["itemid"])
			// determine history type from value_type (fallback to 0)
			histType := 0
			if vt, ok := item["value_type"]; ok {
				switch v := vt.(type) {
				case float64:
					histType = int(v)
				case int:
					histType = v
				default:
					if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { histType = v2 }
				}
			}
			log.Printf("[DEBUG] Using history type %d for itemid %s", histType, itemid)
			if val, err := getLastHistoryValue(apiUrl, token, itemid, histType); err == nil {
				if val != "" {
					// try to format as float with 2 decimals
					if f, ferr := strconv.ParseFloat(strings.TrimSpace(val), 64); ferr == nil {
						nvps = fmt.Sprintf("%.2f", f)
					} else {
						nvps = val
					}
				} else {
					nvps = "Sem histórico"
				}
			} else {
				log.Printf("[ERROR] history.get failed for itemid %s: %v", itemid, err)
				nvps = "Erro ao ler histórico"
			}
		} else {
			nvps = "Criar chave zabbix[requiredperformance] no Template do Zabbix"
		}
	} else {
		log.Printf("[ERROR] item.get failed for requiredperformance: %v", err)
		nvps = "Erro ao procurar item"
	}

	//get Hosts
	if progressCb != nil { progressCb("Coletando informações de Hosts...") }

    
	hostsResp, err := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{ "output": "hostid" })
	if err != nil { return "", err }
	totalHosts := hostsResp["result"].([]interface{})
	nTotalHosts := len(totalHosts)

	enabledResp, err := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{ "output": "hostid", "filter": map[string]interface{}{ "status": 0 } })
	enabledHosts := []interface{}{}
	if err == nil { enabledHosts = enabledResp["result"].([]interface{}) }
	nEnabledHosts := len(enabledHosts)

	disabledResp, err := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{ "output": "hostid", "filter": map[string]interface{}{ "status": 1 } })
	disabledHosts := []interface{}{}
	if err == nil { disabledHosts = disabledResp["result"].([]interface{}) }
	nDisabledHosts := len(disabledHosts)

	// get templates
	if progressCb != nil { progressCb("Coletando informações de Templates...") }
	templatesCount := "N/A"
	templatesCountResp, err := zabbixApiRequest(apiUrl, token, "template.get", map[string]interface{}{
		"countOutput": true,
	})
	if err == nil {
		templatesCount = fmt.Sprintf("%v", templatesCountResp["result"])
	}

	// get total de items
	if progressCb != nil { progressCb("Coletando informações de Items...") }
	nItemsTotal := "-"
	nItemsEnabled := "-"
	nItemsDisabled := "-"	
	itemsTotalResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"output": "extend",
		"countOutput": true,
		"templated": false,
		"webitems": true,
	})
	if err == nil {
		nItemsTotal = fmt.Sprintf("%v", itemsTotalResp["result"])
	}
	// get em itens habilitados 
	itemsEnabledResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"countOutput": true,
		"monitored": true,
		"templated": false,
		"webitems": true,
		"filter": map[string]interface{}{ "status": 0, "state": 0 },
	})
	if err == nil {
		nItemsEnabled = fmt.Sprintf("%v", itemsEnabledResp["result"])
	}
	// get em itens desabilitados
	itemsDisabledResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"countOutput": true,
		"templated": false,
		"webitems": true,
		"filter": map[string]interface{}{ "status": 1 },
	})
	if err == nil {
		nItemsDisabled = fmt.Sprintf("%v", itemsDisabledResp["result"])
	}

	// get em Items não suportados por host ID
	if progressCb != nil { progressCb("Coletando items não suportados...") }
	itemsResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"output": []string{"itemid","name","templateid","error","key_"},
		"filter": map[string]interface{}{ "state": 1 },
		"webitems": 1,
		"selectHosts": []string{"name","hostid"},
		"inherited": true,
	})
	items := []interface{}{}
	if err == nil { items = itemsResp["result"].([]interface{}) }

	// Buscar nome do template real de cada item (usar templateid como chave)
	// Primeiro crio a lista única de templateids, para retornados nos itens não suportados
	templateFakeSet := map[string]struct{}{}
	for _, i := range items {
		item := i.(map[string]interface{})
		tplId := fmt.Sprintf("%v", item["templateid"])
		if tplId == "0" { continue }
		templateFakeSet[tplId] = struct{}{}
	}
	templateFakeIds := []string{}
	for id := range templateFakeSet { templateFakeIds = append(templateFakeIds, id) }

	// cacheTemplateItems maps templateid (fake) -> template host name (o nome do template)
	cacheTemplateItems := map[string]string{}
	cacheTemplateHostID := map[string]string{}
	if len(templateFakeIds) > 0 {
		cacheResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
			"output": []string{"name", "key_", "templateid"},
			"itemids": templateFakeIds,
			"selectHosts": []string{"name", "hostid"},
		})
		if err == nil {
			for _, item := range cacheResp["result"].([]interface{}) {
				itemMap := item.(map[string]interface{})
				itemid := fmt.Sprintf("%v", itemMap["itemid"]) 
				hostsArr, _ := itemMap["hosts"].([]interface{})
				templateName := ""
				templateHostId := ""
				if len(hostsArr) > 0 {
					host := hostsArr[0].(map[string]interface{})
					templateName = fmt.Sprintf("%v", host["name"])
					templateHostId = fmt.Sprintf("%v", host["hostid"]) 
				}
				cacheTemplateItems[itemid] = templateName
				cacheTemplateHostID[itemid] = templateHostId
			}
		}
	}

	// Agrupamentos
	templateCounter := map[string]int{}
	hostCounter := map[string]int{}
	itemCounter := map[string]int{}
	errorCounter := map[string]int{}
	templateItems := map[string][][]string{}
	hostItems := map[string][][]string{}
	for _, i := range items {
		item := i.(map[string]interface{})
		tplId := fmt.Sprintf("%v", item["templateid"])
		if tplId == "0" { continue }
		itemId := fmt.Sprintf("%v", item["itemid"])
		itemName := fmt.Sprintf("%v", item["name"])
		itemError := fmt.Sprintf("%v", item["error"])
		hostsArr := item["hosts"].([]interface{})
		itemHostName := ""
		itemHostId := ""
		if len(hostsArr) > 0 {
			host := hostsArr[0].(map[string]interface{})
			itemHostName = fmt.Sprintf("%v", host["name"])
			itemHostId = fmt.Sprintf("%v", host["hostid"])
		}
		// Inclui as urls na guia de templates, para utilizar como link de acesso rápido para análise e correção dos itens. O link é construído de forma diferente para Zabbix 7 (item.list com filtros com context host) e Zabbix 6 (items.php com hostid+itemid).
		urlEdit := ""
		if majorV >= 7 {
			escItem := neturl.QueryEscape(itemName)
			perPath := fmt.Sprintf("zabbix.php?action=item.list&context=host&filter_hostids%%5B%%5D=%s&filter_name=%s&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_status=-1&filter_with_triggers=-1&filter_inherited=-1&filter_set=1", itemHostId, escItem)
			urlEdit = ambienteUrl + "/" + perPath
		} else {
			urlEdit = url + "/items.php?form=update&hostid=" + itemHostId + "&itemid=" + itemId + "&context=host"
		}
		// usar templateid (tplId) como chave e recuperar o nome do cache
		templateName := cacheTemplateItems[tplId]
		if templateName == "" {
			// fallback: se cache não tiver, tentar buscar pelo itemid (compatibilidade)
			templateName = cacheTemplateItems[itemId]
		}
		// capturo o "templateid"] para ser ID do ITEM dentro do template, para funcionar a logica do template ofensor, isso é necessario para múltiplos itens do mesmo template.
		realTplId := cacheTemplateHostID[tplId]
		if realTplId == "" { realTplId = tplId } // fallback se cache não tiver o mapeamento
		templateCounter[realTplId]++
		hostCounter[itemHostName]++
		itemCounter[itemName+"|"+realTplId]++
		errorCounter[itemError+"|"+realTplId]++
		templateItems[realTplId] = append(templateItems[realTplId], []string{itemName, itemError, itemHostName, urlEdit, templateName})
		hostItems[itemHostName] = append(hostItems[itemHostName], []string{itemName, itemError, realTplId, urlEdit})
	}

	// Buscar nomes dos templates
	templateNames := map[string]string{}
	if len(templateCounter) > 0 {
		tplIds := []string{}
		for tplId := range templateCounter { tplIds = append(tplIds, tplId) }
		tplResp, err := zabbixApiRequest(apiUrl, token, "template.get", map[string]interface{}{
			"output": []string{"templateid","name"},
			"templateids": tplIds,
		})
		if err == nil {
			for _, tpl := range tplResp["result"].([]interface{}) {
				tplMap := tpl.(map[string]interface{})
				templateNames[fmt.Sprintf("%v", tplMap["templateid"])] = fmt.Sprintf("%v", tplMap["name"])
			}
		}
		// preencha nomes a partir do cache também (fallback/compatibilidade)
		for id, name := range cacheTemplateItems { if templateNames[id] == "" { templateNames[id] = name } }
	}

	// Top N
	topN := 10
	type kv struct{ Key string; Value int }
	sortMap := func(m map[string]int) []kv {
		arr := []kv{}
		for k, v := range m { arr = append(arr, kv{k, v}) }
		sort.Slice(arr, func(i, j int) bool { return arr[i].Value > arr[j].Value })
		return arr
	}
	topTemplates := sortMap(templateCounter)
	if len(topTemplates) > topN { topTemplates = topTemplates[:topN] }
	topHosts := sortMap(hostCounter)
	if len(topHosts) > topN { topHosts = topHosts[:topN] }
	topItems := sortMap(itemCounter)
	if len(topItems) > topN { topItems = topItems[:topN] }
	topErrors := sortMap(errorCounter)
	if len(topErrors) > topN { topErrors = topErrors[:topN] }

	// Descrições (textos exibidos nas view-boxes)
	descTemplates := "Revise os templates com maior número de itens problemáticos. Verifique se estão atualizados, compatíveis com a versão do Zabbix e se os itens monitorados ainda fazem sentido para o ambiente. Considere simplificar ou dividir templates muito grandes."
	descHosts := "Analise os hosts com mais erros. Verifique conectividade, permissões, agentes instalados e se o host está ativo. Corrija configurações específicas ou remova hosts obsoletos do monitoramento."
	descItems := "Items recorrentes podem indicar falhas de configuração, incompatibilidade ou ausência de recursos no host. Revise a chave do item, parâmetros, dependências externas (scripts, drivers, diretórios) e ajuste o template conforme necessário."
	descErros := "Para cada tipo de erro, consulte a documentação do Zabbix e do sistema operacional/serviço monitorado. Corrija chaves inválidas, permissões, dependências, drivers ou scripts ausentes. Ajuste preprocessamento e tipos de dados conforme o erro apresentado. Para itens SNMP, verifique se o firmware do equipamento está atualizado, valide o OID utilizado e confirme se a comunidade SNMP está correta e configurada no dispositivo."
	descDetalhamento := "Analise cada item e erro detalhado. Acesse o link para editar o item diretamente no Zabbix, revise a configuração, ajuste parâmetros e valide se o item é realmente necessário."
	descNaoSuportados := "Os itens não suportados são aqueles que estão ativos, porém no momento de efetuar a coleta houve um erro. Esses itens continuam consumindo os processos do Zabbix desnecessariamente. Clique no link e analise caso a caso para correção. Items que não são mais necessários devem ser removidos para otimizar o desempenho do Zabbix."

	// --- HTML moderno ---
	html := `<div class='zabbix-report-modern'>`
		// Global tooltip CSS/JS (single copy) - info-icon + info-tooltip
		html += `<style>
		.info-icon{display:inline-flex;align-items:center;justify-content:center;width:18px;height:18px;cursor:pointer;margin-left:6px;position:relative}
		.info-icon svg{display:block}
		.info-tooltip{display:none;position:absolute;z-index:40;left:22px;top:50%;transform:translateY(-50%);background:#e3f2fd;color:#102a43;padding:8px 12px;border-radius:8px;box-shadow:0 6px 24px rgba(0,0,0,0.12);font-size:13px;min-width:360px;max-width:auto;white-space:normal;word-break:normal;overflow-wrap:break-word}
		.info-icon:focus .info-tooltip, .info-icon:hover .info-tooltip{display:block}
		</style>
		<script>
		function setupInfoTooltips(){
			document.querySelectorAll('.info-icon').forEach(function(icon){
				if(icon._tooltipBound) return; icon._tooltipBound = true;
				icon.addEventListener('click', function(e){
					var tip = this.querySelector('.info-tooltip');
					if(tip){ tip.style.display = (tip.style.display==='block') ? 'none' : 'block'; }
					e.stopPropagation();
				});
			});
		}
		document.addEventListener('click', function(){ document.querySelectorAll('.info-tooltip').forEach(function(t){ t.style.display='none'; }); });
		// call once now and again after dynamic inserts
		setTimeout(setupInfoTooltips, 10);
		</script>`

		// helper to render headings with info icon and tooltip
		titleWithInfo := func(tag, title, tip string) string {
				// build SVG question-circle and tooltip content
				sv := `<svg viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='1.6' fill='white'/><text x='8' y='11' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>`
			tipEsc := htmlpkg.EscapeString(tip) // Escape the tooltip text for HTML
				// place tooltip inside the icon span so positioning is relative
				return fmt.Sprintf("<%s>%s <span class='info-icon' tabindex='0'>%s<span class='info-tooltip'>%s</span></span></%s>", tag, htmlpkg.EscapeString(title), sv, tipEsc, tag)
		}
	// compute ambiente and version for header (ambienteUrl precomputed above)
	// ambienteUrl is already set near the top of the function to avoid undefined usage
	verLabel := "N/A"
	if zabbixVersion != "" { verLabel = zabbixVersion }

	// Tabs UI (UX: simpler navigation, grouped content)
	html += `<style>` +
		`.tabs-container{display:flex;gap:8px;flex-wrap:nowrap;overflow-x:auto;margin-top:12px;margin-bottom:14px;}` +
		`.tab-btn{padding:10px 14px;border-radius:6px;border:1px solid #d1d5db;background:#ffffff;color:#102a43;font-weight:600;cursor:pointer;white-space:nowrap;}` +
		`.tab-btn.active{background:#0b69d6;color:#fff;border-color:#0b69d6}` +
		`.tab-panel{padding-top:12px;}` +
	`</style>`
	html += `<div style='display:flex;align-items:center;justify-content:space-between;gap:12px;'>`
	html += `<div style='font-size:14px;color:#1f2937;'><strong>Ambiente:</strong> ` + htmlpkg.EscapeString(ambienteUrl) + `</div>`
	html += `<div style='font-size:14px;color:#1f2937;'><strong>Versão:</strong> ` + htmlpkg.EscapeString(verLabel) + `</div>`
	html += `</div>`
	html += `<div class='tabs-container'>`
	html += `<button class='tab-btn active' data-tab='tab-resumo'>Resumo do Ambiente</button>`
	html += `<button class='tab-btn' data-tab='tab-processos'>Zabbix Server</button>`
	html += `<button class='tab-btn' data-tab='tab-proxys'>Zabbix Proxys</button>`
	html += `<button class='tab-btn' data-tab='tab-items'>Items e LLDs</button>`
	html += `<button class='tab-btn' data-tab='tab-templates'>Templates</button>`
	html += `<button class='tab-btn' data-tab='tab-top'>Top Hosts/Templates/Items</button>`	
	html += `<button class='tab-btn' data-tab='tab-recomendacoes'>Recomendações</button>`
	html += `</div>`

	// Tab panels: resumo (visible), others hidden by default
	html += `<div id='tab-resumo' class='tab-panel' style='display:block;'>`
	html += `<h2 class='tab-print-title'>Resumo do Ambiente</h2>`
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Parâmetro</th><th>Valor</th><th>Detalhes</th></tr></thead><tbody>`
	// Hosts
	html += `<tr><td>Número de hosts (habilitados/desabilitados)</td><td>` + fmt.Sprintf("%d", nTotalHosts) + `</td><td>` + fmt.Sprintf("%d / %d", nEnabledHosts, nDisabledHosts) + `</td></tr>`
	// Templates
	html += `<tr><td>Número de templates</td><td>` + templatesCount + `</td><td></td></tr>`
	// Items
	html += `<tr><td>Número de items (habilitados/desabilitados/não suportados)</td><td>` + nItemsTotal + `</td><td>` + nItemsEnabled + ` / ` + nItemsDisabled + ` / ` + nItemsNaoSuportados + `</td></tr>`
	// Proxys
	if progressCb != nil { progressCb("Coletando informações de Proxys...") }
	proxyCount := 0
	var proxies []map[string]interface{}
	if pc, perr := getProxyCount(apiUrl, token); perr == nil { proxyCount = pc } else { log.Printf("[ERROR] proxy.get failed: %v", perr) }
	// fetch full proxy list to render names and types
	if plist, perr2 := getProxies(apiUrl, token); perr2 == nil && plist != nil { proxies = plist } else if perr2 != nil { log.Printf("[ERROR] proxy.get (list) failed: %v", perr2) }
	html += `<tr><td>Número de Proxys</td><td>` + fmt.Sprintf("%d", proxyCount) + `</td><td></td></tr>`
	// Usuários
	html += `<tr><td>Número de usuários</td><td>` + fmt.Sprintf("%d", nUsers) + `</td><td></td></tr>`
	// NVPS
	html += `<tr><td>Required server performance, new values per second</td><td>` + nvps + `</td><td></td></tr>`
	html += `</tbody></table></div>`

	// descriptions for tooltip (lowercase key) - moved here so pollers section can reference it
	procDesc := map[string]string{
		"agent poller": `Parâmetro "StartPollers": especifica o número de processadores de coleta (pollers) para itens passivos. Aumente este valor para elevar a taxa de processamento de verificações passivas.`,
		"alert manager": `Parâmetro "AlertManager": gerencia a fila de alertas e coordena o encaminhamento para o serviço de envio de notificações. Ajuste quando houver atraso no processamento de alertas.`,
		"alert syncer": `Parâmetro "AlertSyncer": responsável por gravar alertas na base de dados; aumente se há acúmulo de gravações.`,
		"alerter": `Parâmetro "Alerter": processo responsável pelo envio efetivo de notificações (e-mail, SMS, etc.); aumente para melhorar paralelismo no envio.`,
		"availability manager": `Parâmetro "AvailabilityManager": atualiza o estado de disponibilidade de hosts; relevante quando há muitas mudanças de status.`,
		"browser poller": `Parâmetro "StartBrowserPollers": poller responsável por verificações de navegador (web scenarios). Aumente para mais paralelismo em testes de páginas web.`,
		"configuration syncer": `Parâmetro "ConfigurationSyncer": sincroniza a configuração em memória do servidor Zabbix; aumente se há demora na aplicação de alterações de configuração.`,
		"configuration syncer worker": `Parâmetro "ConfigurationSyncerWorker": worker do Configuration Syncer que resolve macros de usuário e sincroniza nomes/valores; ajuste quando houver alto volume de mudanças de macros.`,
		"connector manager": `Parâmetro "ConnectorManager": gerencia conectores externos e enfileiramento de requisições de integração com serviços terceiros; dimensione conforme integrações ativas.`,
		"connector worker": `Parâmetro "ConnectorWorker": processos trabalhadores do conector que executam requisições e tratam respostas de sistemas externos; aumente se integrações ficarem lentas.`,
		"discovery manager": `Parâmetro "StartDiscoverers": gerencia tarefas de descoberta de hosts/serviços em redes; aumente para acelerar varreduras em grandes ambientes.`,
		"discovery worker": `Parâmetro "DiscoveryWorker": trabalhadores de descoberta que executam verificações (SNMP, ICMP, etc.) solicitadas pelo discovery manager; escale conforme necessidade de varreduras.`,
		"escalator": `Parâmetro "Escalator": processo de escalonamento de ações automatizadas (ex.: escalonamento de alertas); relevante em regras de escalonamento complexas.`,
		"ha manager": `Parâmetro "HAManager": gerencia recursos de alta disponibilidade entre servidores Zabbix em cluster; ajuste quando usar HA e notar latência na sincronização.`,
		"history poller": `Parâmetro "StartPollers": pollers que coletam dados calculados ou requerem conexão com o banco; aumente se cálculo de itens estiver atrasado.`,
		"history syncer": `Parâmetro "HistorySyncer": escritor de histórico que persiste dados de itens no banco de dados; Controlado pelo StartDBSyncer, normalmente é 1 DBSyncer para cada 1000 Nvps`,
		"housekeeper": `Parâmetro "HousekeeperFrequency": remove dados históricos antigos e limpa tabelas; aumente se limpeza estiver lenta e o banco ficar grande. Possui total relação com o Banco de Dados, se é particionado ou não.`,
		"http agent poller": `Parâmetro "StartHTTPPollers": poller assíncrono para checagens HTTP com threads de trabalho; aumente para maior concorrência em sondagens web.`,
		"http poller": `Parâmetro "StartHTTPPollers": poller para monitoramento HTTP síncrono; ajuste conforme volume de web checks.`,
		"icmp pinger": `Parâmetro "StartPingers": pollers de ICMP (ping) para verificar disponibilidade de hosts; aumente se muitos hosts necessitam de verificação frequente.`,
		"internal poller": `Parâmetro "InternalPollers": poller para verificações internas do servidor Zabbix (saúde e métricas internas); ajuste se coleta interna estiver sobrecarregada.`,
		"ipmi manager": `Parâmetro "IPMIManager": gerencia pollers IPMI para coletar métricas via IPMI; dimensione quando houver muitos dispositivos IPMI.`,
		"ipmi poller": `Parâmetro "IPMIPollers": poller que executa checagens IPMI em equipamentos; aumente para maior paralelismo em ambientes com IPMI extensivo.`,
		"java poller": `Parâmetro "JavaPollers": poller para coletas do tipo Java; ajuste quando houver muitos checadores baseados em Java.`,
		"lld manager": `Parâmetro "LLDManager": gerencia tarefas de Low-Level Discovery (LLD) que detectam automaticamente serviços/instâncias; aumente para acelerar descobertas em larga escala.`,
		"lld worker": `Parâmetro "LLDWorkers": workers do LLD que executam regras de descoberta e geram itens/dispositivos; dimensione conforme a frequência e quantidade de regras LLD.`,
		"odbc poller": `Parâmetro "ODBCPollers": poller responsável por consultas ODBC/DB para coleta de métricas; aumente se houver muitos checks dependentes de ODBC.`,
		"poller": `Parâmetro "StartPollers": processadores de coleta padrão para itens passivos; afeta throughput geral de coleta.`,
		"preprocessing manager": `Parâmetro "PreprocessingManager": gerencia tarefas de pré-processamento de dados (regexp, transform) antes de serem salvo em banco de dados; aumente para reduzir filas de processamento.`,
		"preprocessing worker": `Parâmetro "PreprocessingWorkers": threads que executam transformações/preprocessamento de valores antes de armazenamento ou avaliação de triggers.`,
		"proxy poller": `Parâmetro "ProxyPollers": pollers para proxies remotos que agregam dados de agentes/proxies; ajuste para balancear carga de proxies.`,
		"proxy group manager": `Parâmetro "ProxyGroupManager": gerencia balanceamento e alta disponibilidade entre proxies; dimensione se usar múltiplos proxies para redundância.`,
		"report manager": `Parâmetro "ReportManager": gerencia tarefas agendadas de geração de relatórios; aumente se houver acúmulo na geração de relatórios.`,
		"report writer": `Parâmetro "ReportWriter": processos responsáveis por gerar e gravar relatórios programados; aumente quando a produção de relatórios estiver atrasada.`,
		"self-monitoring": `Parâmetro "SelfMonitoring": coleta métricas internas do servidor (uso CPU, filas internas); importante para avaliação de saúde do Zabbix Server.`,
		"service manager": `Parâmetro "ServiceManager": gerencia eventos de serviço, tags e recuperação de problemas vindo de vários componentes; escale se processamento de eventos estiver lento.`,
		"snmp poller": `Parâmetro "StartSNMPPollers": poller assíncrono para checagens SNMP (walk/get) com threads; aumente para maior concorrência em ambientes SNMP extensos.`,
		"snmp trapper": `Parâmetro "SNMPTrapper": componente que recebe e processa traps SNMP; aumente para maior taxa de ingestão de traps.`,
		"task manager": `Parâmetro "TaskManager": executa tarefas remotas solicitadas por outros componentes (fechar problema, executar comando remoto, etc.); aumente para reduzir latência de execução de tarefas.`,
		"timer": `Parâmetro "Timer": responsável pelo agendamento de tarefas periódicas e manutenção; ajuste se agendamentos estiverem atrasados.`,
		"trapper": `Parâmetro "StartTrappers": processo que recebe itens ativos, traps e comunicação de proxies; dimensione para aumentar ingestão de dados ativos.`,
		"trigger housekeeper": `Parâmetro "TriggerHousekeeper": remove problemas/triggers órfãos ou deletados; aumente se houver acúmulo de entradas a limpar.`,
		"unreachable poller": `Parâmetro "StartPollersUnreachable": poller específico para hosts considerados inatingíveis; hosts sem comunicação. Os parâmetros UnreachableDelay e UnreachablePeriod podem ser utilizados para verificar se o host está com comunicação e retirar desta fila. Queda massiva de hosts, pode afetar este poller.`,
		"vmware collector": `Parâmetro "StartVMwareCollectors": coletor para integrações VMware responsável por consultar APIs VMware; aumente para maior paralelismo em ambientes virtualizados grandes.`,
		"data sender": `Processo exclusivo do Zabbix Proxy: responsável por enviar os dados coletados ao Zabbix Server. Sobrecarga indica que a conexão com o servidor está lenta, há muitos dados acumulados na fila ou o parâmetro DataSenderFrequency precisa ser ajustado.`,
	}

	// Prepare numeric totals for gauge
	totalItemsVal := 0
	unsupportedVal := 0
	// itemsNoTplCount será preenchido posteriormente (Items sem Template)
	itemsNoTplCount := 0
	if nItemsTotal != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItemsTotal)); err == nil { totalItemsVal = v }
	}
	if nItemsNaoSuportados != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItemsNaoSuportados)); err == nil { unsupportedVal = v }
	}
	if totalItemsVal < 0 { totalItemsVal = 0 }
	if unsupportedVal < 0 { unsupportedVal = 0 }
	supportedVal := totalItemsVal - unsupportedVal
	if supportedVal < 0 { supportedVal = 0 }

	// Gauge area (pie/doughnut) - reserve space for multiple gauges later
	html += `<div class='summary-gauges' style='display:flex;gap:18px;flex-wrap:wrap;margin-top:12px;align-items:flex-start;'>`
	// Hosts gauge (left)
	html += `<div class='card' style='background:#fff;color:#222;padding:12px;border-radius:8px;min-width:220px;box-shadow:0 1px 6px rgba(0,0,0,0.04);'>`
	html += `<h4 style='margin:0 0 8px 0;'>Hosts: Desabilitados</h4>`
	html += `<canvas id='hosts-gauge' width='200' height='200' style='max-width:200px;' data-total='` + fmt.Sprintf("%d", nTotalHosts) + `' data-unsupported='` + fmt.Sprintf("%d", nDisabledHosts) + `' data-unsupported-label='Desabilitados' data-supported-label='Habilitados' data-color-unsupported='#ffcc66' data-color-supported='#66c2a5'></canvas>`
	// legend lines: color swatches and separated lines (supported / disabled)
	hostDisabledPct := 0.0
	if nTotalHosts > 0 { hostDisabledPct = (float64(nDisabledHosts) * 100.0) / float64(nTotalHosts) }
	html += `<div class='gauge-legend' style='width:100%;margin-top:8px;font-size:0.95em;'>`
	html += `<div style='display:flex;align-items:center;gap:8px;margin-bottom:6px;'><span style='display:inline-block;width:12px;height:12px;background:#66c2a5;border-radius:3px;'></span><strong>Total hosts:</strong>&nbsp;` + fmt.Sprintf("%d", nTotalHosts) + `</div>`
	html += `<div style='display:flex;align-items:center;gap:8px;'><span style='display:inline-block;width:12px;height:12px;background:#ffcc66;border-radius:3px;'></span><strong>Desabilitados:</strong>&nbsp;` + fmt.Sprintf("%d", nDisabledHosts) + ` (` + fmt.Sprintf("%.2f", hostDisabledPct) + `%)</div>`
	html += `</div>`
	html += `</div>`
	// Items gauge (right)
	html += `<div class='card' style='background:#fff;color:#222;padding:12px;border-radius:8px;min-width:220px;box-shadow:0 1px 6px rgba(0,0,0,0.04);'>`
	html += `<h4 style='margin:0 0 8px 0;'>Items: Não Suportados</h4>`
	html += `<canvas id='items-gauge' width='200' height='200' style='max-width:200px;' data-total='` + fmt.Sprintf("%d", totalItemsVal) + `' data-unsupported='` + fmt.Sprintf("%d", unsupportedVal) + `' data-unsupported-label='Não suportados' data-supported-label='Suportados' data-color-unsupported='#ff7a7a' data-color-supported='#66c2a5'></canvas>`
	// legend lines for items
	itemsUnsupportedPct := 0.0
	if totalItemsVal > 0 { itemsUnsupportedPct = (float64(unsupportedVal) * 100.0) / float64(totalItemsVal) }
	html += `<div class='gauge-legend' style='width:100%;margin-top:8px;font-size:0.95em;'>`
	html += `<div style='display:flex;align-items:center;gap:8px;margin-bottom:6px;'><span style='display:inline-block;width:12px;height:12px;background:#66c2a5;border-radius:3px;'></span><strong>Total items:</strong>&nbsp;` + fmt.Sprintf("%d", totalItemsVal) + `</div>`
	html += `<div style='display:flex;align-items:center;gap:8px;'><span style='display:inline-block;width:12px;height:12px;background:#ff7a7a;border-radius:3px;'></span><strong>Não suportados:</strong>&nbsp;` + fmt.Sprintf("%d", unsupportedVal) + ` (` + fmt.Sprintf("%.2f", itemsUnsupportedPct) + `%)</div>`
	html += `</div>`
	html += `</div>`
	html += `</div>`


	html += `</div>` // end tab-resumo

	// --- Processos e Threads Zabbix Server (Pollers + Internal) ---
		if progressCb != nil { progressCb("Coletando informações de Pollers e Processos internos...") }
		// Get CHECKTRENDTIME as string for display (default "30")
	       checkTrendStr := os.Getenv("CHECKTRENDTIME")
	       if checkTrendStr == "" { checkTrendStr = "15d" }
	       // Extract numeric part (days/hours/minutes)
	       checkTrendDisplay := "30 dias"
	       if len(checkTrendStr) > 1 {
		       unit := checkTrendStr[len(checkTrendStr)-1]
		       numPart := checkTrendStr[:len(checkTrendStr)-1]
		       if unit == 'd' {
			       checkTrendDisplay = numPart + " dias"
		       } else if unit == 'h' {
			       checkTrendDisplay = numPart + " horas"
		       } else if unit == 'm' {
			       checkTrendDisplay = numPart + " minutos"
		       } else {
			       checkTrendDisplay = checkTrendStr + " (minutos)"
		       }
	       }
	       html += `<div id='tab-processos' class='tab-panel' style='display:none;'>`
	       html += `<h2 class='tab-print-title'>Zabbix Server</h2>`
	       serverHost := os.Getenv("ZABBIX_SERVER_HOSTID")
	       if serverHost == "" { serverHost = "10084" }
	       log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID=%s will be used for pollers", serverHost)
	       // build poller list conditionally based on Zabbix major version
	       pollerNames := []string{}
	       // pollers available in both 6 and 7
	       commonPollers := []string{
		       "poller",
		       "http poller",
		       "icmp pinger",
		       "ipmi poller",
		       "java poller",
		       "odbc poller",
		       "proxy poller",
		       "unreachable poller",
		       "preprocessing worker",
		       "preprocessing manager",
		       "vmware collector",
	       }
	       pollerNames = append(pollerNames, commonPollers...)
	       // pollers introduced / better represented in Zabbix 7
	       if majorV >= 7 {
		       pollerNames = append([]string{`agent poller`, `browser poller`, `http agent poller`, `snmp poller`}, pollerNames...)
	       } else {
		       // for Zabbix 6, include SNMP trapper as separate component if desired (kept out of pollers)
	       }
	// procNames defined here so both sections share the same bulk fetch below
	procNames := []string{
		"configuration syncer",
		"configuration syncer worker",
		"alerter",
		"alert manager",
		"availability manager",
		"escalator",
		"history syncer",
		"housekeeper",
		"trapper",
		"ipmi manager",
		"lld manager",
		"lld worker",
		"preprocessing manager",
		"preprocessing worker",
		"report manager",
		"report writer",
		"self-monitoring",
		"service manager",
		"task manager",
		"timer",
		"trigger housekeeper",
		"vmware collector",
		"ha manager",
	}
	// Single bulk item.get for ALL server process items (pollers + internal processes)
	allServerNames := append(append([]string{}, pollerNames...), procNames...)
	serverItemsMap, serverItemsErr := getProcessItemsBulk(apiUrl, token, allServerNames, serverHost)
	if serverItemsErr != nil {
		log.Printf("[ERROR] bulk process item.get failed: %v", serverItemsErr)
		serverItemsMap = map[string]map[string]interface{}{}
	}
	log.Printf("[DEBUG] bulk process item.get: %d matches for %d names", len(serverItemsMap), len(allServerNames))
	// Check host existence once — reused in DisabledMsg across both goroutine loops
	serverHostExists := false
	if serverHost != "" {
		hostParams := map[string]interface{}{"output": []string{"hostid"}, "hostids": []string{serverHost}}
		if hresp, herr := zabbixApiRequest(apiUrl, token, "host.get", hostParams); herr == nil {
			if rr, ok := hresp["result"]; ok {
				if arr, ok2 := rr.([]interface{}); ok2 && len(arr) > 0 { serverHostExists = true }
			}
		}
	}
		html += titleWithInfo("h3", "Pollers (Data Collectors)", `Os pollers (de forma passiva) consultam ativamente os agentes configurados, em intervalos definidos para coletar as métricas. Isso contrasta com o modo passivo (trappers), onde os agentes enviam dados automaticamente ao servidor; porém eles também podem ser sobrecarregados quando há aumento de fila. Para otimizar, aumente gradualmente o número de Pollers no arquivo zabbix_server.conf quando houver degradação. As decisões de ajuste devem basear-se nas tendências dos últimos ` + checkTrendDisplay + `: se a utilização média estiver consistentemente entre 50% e 60% e os picos ultrapassarem 60%, considere aumentar os pollers; se estiver abaixo de 50%, normalmente não há necessidade de aumento.`)		
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Poller</th><th>value_min</th><th>value_avg</th><th>value_max</th><th>Status</th></tr></thead><tbody>`
	type pollRow struct{
		Friendly string
		Desc string
		Smin string
		Savg string
		Smax string
		Vmax float64
		Vavg float64
		StatusText string
		StatusStyle string
		Disabled bool
		DisabledMsg string
		Err bool
	}
	// Parallelize poller collection using shared semaphore `sem`
	pollRows := []pollRow{}
	type pollRes struct{ Idx int; Row pollRow }
	resultsPoll := make(chan pollRes, len(pollerNames))
	var wgPoll sync.WaitGroup
	for idx, name := range pollerNames {
		idx, name := idx, name
		wgPoll.Add(1)
		go func() {
			defer wgPoll.Done()
			sem <- struct{}{}
			defer func(){ <-sem }()

			baseName := strings.ToLower(strings.TrimSpace(name))
			desc := procDesc[baseName]
			if desc == "" { desc = "Poller process" }
			words := strings.Fields(name)
			for i, w := range words { tw := strings.TrimSpace(w); if len(tw) > 0 { words[i] = strings.ToUpper(tw[:1]) + strings.ToLower(tw[1:]) } }
			friendly := strings.Join(words, " ")

			pr := pollRow{Friendly: friendly, Desc: desc, Disabled: false, Err: false, Vmax: -1}

			item := serverItemsMap[strings.ToLower(strings.TrimSpace(name))]
			if item == nil {
				pr.Disabled = true
				if serverHost != "" && !serverHostExists {
					pr.DisabledMsg = fmt.Sprintf("Hostid %s não encontrado, informe o valor na ENV ZABBIX_SERVER_HOSTID.", serverHost)
				} else if majorV < 7 {
					switch strings.ToLower(strings.TrimSpace(name)) {
					// para Zabbix 6, alguns pollers e workers não existem para versão, este é um bypass.
					case "agent poller", "browser poller", "http agent poller", "snmp poller", "configuration syncer worker":
						pr.DisabledMsg = "Não existe nesta versão do Zabbix"
					default:
						pr.DisabledMsg = "Processo não habilitado"
					}
				} else {
					pr.DisabledMsg = "Processo não habilitado"
				}
				resultsPoll <- pollRes{Idx: idx, Row: pr}
				return
			}
			itemid := fmt.Sprintf("%v", item["itemid"])
			log.Printf("[DEBUG] poller '%s': key_=%v itemid=%s", name, item["key_"], itemid)
			trend, terr := getLastTrend(apiUrl, token, itemid, 30)
			if terr != nil {
				log.Printf("[DEBUG] trend.get failed for poller '%s' (itemid=%s): %v — falling back to history.get", name, itemid, terr)
				trend = nil // força fallback para history abaixo
			}
			if trend == nil {
				// fallback: busca history quando trends não estão disponíveis (trends=0 no item ou erro)
				histType := 0 // float para itens zabbix[process,...]
				if vt := fmt.Sprintf("%v", item["value_type"]); vt == "3" { histType = 3 }
				var herr error
				trend, herr = getHistoryStats(apiUrl, token, itemid, histType, 30)
				if herr != nil {
					log.Printf("[DEBUG] history.get also failed for poller '%s' (itemid=%s): %v — marking disabled", name, itemid, herr)
					pr.Disabled = true
					pr.DisabledMsg = "Processo não habilitado"
					resultsPoll <- pollRes{Idx: idx, Row: pr}
					return
				}
				if trend == nil {
					pr.Disabled = true
					pr.DisabledMsg = "Processo não habilitado"
					resultsPoll <- pollRes{Idx: idx, Row: pr}
					return
				}
			}
			parseVal := func(k string) float64 {
				if v, ok := trend[k]; ok {
					s := fmt.Sprintf("%v", v)
					if f, err := strconv.ParseFloat(s, 64); err == nil { return f }
				}
				return -1
			}
			vmin := parseVal("value_min")
			vavg := parseVal("value_avg")
			vmax := parseVal("value_max")
			pr.Vmax = vmax
			pr.Vavg = vavg
			fmtVal := func(f float64) string {
				if f < 0 { return "-" }
				return fmt.Sprintf("%.2f%%", f)
			}
			pr.Smin = fmtVal(vmin)
			pr.Savg = fmtVal(vavg)
			pr.Smax = fmtVal(vmax)
			if vavg >= 0 {
				if vavg < 59.9 {
					pr.StatusText = "OK"
					pr.StatusStyle = "background:#66c28a;color:#000;padding:6px;border-radius:4px;text-align:center;"
				} else {
					pr.StatusText = "Atenção"
					pr.StatusStyle = "background:#ff6666;color:#000;padding:6px;border-radius:4px;text-align:center;"
				}
			} else {
				pr.StatusText = "-"
			}
			resultsPoll <- pollRes{Idx: idx, Row: pr}
		}()
	}
	wgPoll.Wait()
	close(resultsPoll)
	tempPoll := make(map[int]pollRow)
	for r := range resultsPoll { tempPoll[r.Idx] = r.Row }
	for i := 0; i < len(pollerNames); i++ { if v, ok := tempPoll[i]; ok { pollRows = append(pollRows, v) } }
	// sort by Vavg desc (items with -1 go last)
	sort.Slice(pollRows, func(i, j int) bool {
		return pollRows[i].Vavg > pollRows[j].Vavg
	})
	// render
	       for _, pr := range pollRows {
		       nameCell := `<td style='position:relative;padding:0;'>` +
		       `<div style='display:flex;align-items:center;gap:4px;'>` +
		       `<span>` + pr.Friendly + `</span>` +
		       `<span class='info-icon' tabindex='0' style='display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;cursor:pointer;outline:none;'>` +
		       `<svg viewBox='0 0 16 16' width='14' height='14' style='display:block;'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='2' fill='white'/><text x='8' y='12' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
			`<span class='info-tooltip' style='display:none;position:absolute;z-index:10;left:22px;top:50%;transform:translateY(-50%);background:#e3f2fd;color:#102a43;padding:7px 12px;border-radius:6px;box-shadow:0 2px 8px rgba(0,0,0,0.08);font-size:13px;min-width:360px;max-width:720px;white-space:normal;overflow:visible;word-break:normal;overflow-wrap:break-word;'>` + htmlpkg.EscapeString(pr.Desc) + `</span>` +
		       `</span>` +
		       `</div></td>`
		       // Adiciona JS/CSS para tooltip interrogação (apenas uma vez, mas seguro repetir)
		       html += `<style>
		       .info-icon:focus .info-tooltip,
		       .info-icon:hover .info-tooltip {
			       display: block;
		       }
		       .info-icon {
			       outline: none;
		       }
			\.info-tooltip {
							transition: opacity 0.15s;
							white-space: normal;
							overflow: visible;
							max-width: 520px;
							word-break: normal;
							overflow-wrap: break-word;
						}
		       </style>
		       <script>
		       function setupInfoTooltips(){
			 document.querySelectorAll('.info-icon').forEach(function(icon){
			       if(icon._tooltipBound) return;
			       icon._tooltipBound = true;
			       icon.addEventListener('click',function(e){
				 var tip = this.querySelector('.info-tooltip');
				 if(tip){ tip.style.display = (tip.style.display==='block') ? 'none' : 'block'; }
				 e.stopPropagation();
			       });
			 });
		       }
		       setupInfoTooltips();
		       document.addEventListener('click',function(){
			 document.querySelectorAll('.info-tooltip').forEach(function(tip){ tip.style.display='none'; });
		       });
		       // Se usar SPA ou renderização dinâmica, chame setupInfoTooltips() após atualizar a tabela
		       </script>`
		       if pr.Err {
			       html += `<tr>` + nameCell + `<td colspan='4'>Erro ao obter dados</td></tr>`
			       continue
		       }
		       if pr.Disabled {
			       dm := "Processo não habilitado"
			       if pr.DisabledMsg != "" { dm = pr.DisabledMsg }
			       html += `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td style='background:#cccccc;color:#000;padding:6px;border-radius:4px;text-align:center;'>` + htmlpkg.EscapeString(dm) + `</td></tr>`
			       continue
		       }
		       html += `<tr>` + nameCell + `<td>` + pr.Smin + `</td><td>` + pr.Savg + `</td><td>` + pr.Smax + `</td><td style='` + pr.StatusStyle + `'>` + pr.StatusText + `</td></tr>`
	       }
	html += `</tbody></table></div>`
	html += titleWithInfo("h3", "Internal Process", `Os processos internos são responsáveis pelo processamento de informações do servidor e impactam o desempenho dos serviços. Para otimizar, aumente gradualmente o número de processos degradados no arquivo zabbix_server.conf. As decisões de ajuste devem basear-se nas tendências dos últimos ` + checkTrendDisplay + `: se a utilização média estiver consistentemente entre 50% e 60% e os picos ultrapassarem 60%, considere aumentar os pollers/processos; se estiver abaixo de 50%, normalmente não há necessidade de aumento.`)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Internal Process</th><th>value_min</th><th>value_avg</th><th>value_max</th><th>Status</th></tr></thead><tbody>`
	// procDesc
	type procRow struct{
		Friendly string
		Desc string
		Smin string
		Savg string
		Smax string
		Vmax float64
		Vavg float64
		StatusText string
		StatusStyle string
		Disabled bool
		DisabledMsg string
		Err bool
	}
	procRows := []procRow{}
	// Parallelize internal process lookups/trends using shared semaphore
	type procResult struct{ idx int; pr procRow }
	results := make(chan procResult, len(procNames))
	var pwg sync.WaitGroup
	for i, name := range procNames {
		i := i; name := name
		pwg.Add(1)
		go func() {
			defer pwg.Done()
			sem <- struct{}{}
			defer func(){ <-sem }()

			// friendly name (title case), with special-case for LLD
			words := strings.Fields(strings.TrimSpace(name))
			for wi, w := range words { tw := strings.TrimSpace(w); if len(tw) > 0 { words[wi] = strings.ToUpper(tw[:1]) + strings.ToLower(tw[1:]) } }
			if len(words) > 0 && strings.ToLower(words[0]) == "lld" { words[0] = "LLD" }
			friendly := strings.Join(words, " ") + " Internal Processes"

			baseName := strings.ToLower(strings.TrimSpace(name))
			desc := procDesc[baseName]
			if desc == "" { desc = "Internal process" }

			pr := procRow{Friendly: friendly, Desc: desc, Disabled: false, Err: false, Vmax: -1}

			item := serverItemsMap[strings.ToLower(strings.TrimSpace(name))]
			if item == nil {
				pr.Disabled = true
				if serverHost != "" && !serverHostExists {
					pr.DisabledMsg = fmt.Sprintf("Hostid %s não encontrado, informe o valor na ENV ZABBIX_SERVER_HOSTID.", serverHost)
				} else if majorV < 7 {
					n := strings.ToLower(strings.TrimSpace(name))
					if n == "lld manager" || n == "lld worker" || n == "configuration syncer worker" {
						pr.DisabledMsg = "Não existe nesta versão do Zabbix"
					} else {
						pr.DisabledMsg = "Processo não habilitado"
					}
				} else {
					pr.DisabledMsg = "Processo não habilitado"
				}
				results <- procResult{idx: i, pr: pr}
				return
			}
			itemid := fmt.Sprintf("%v", item["itemid"])
			log.Printf("[DEBUG] internal process '%s': key_=%v itemid=%s", name, item["key_"], itemid)
			trend, terr := getLastTrend(apiUrl, token, itemid, 30)
			if terr != nil {
				log.Printf("[DEBUG] trend.get failed for process '%s' (itemid=%s): %v — falling back to history.get", name, itemid, terr)
				trend = nil // força fallback para history abaixo
			}
			if trend == nil {
				// fallback: busca history quando trends não estão disponíveis (trends=0 no item ou erro)
				histType := 0 // float para itens zabbix[process,...]
				if vt := fmt.Sprintf("%v", item["value_type"]); vt == "3" { histType = 3 }
				var herr error
				trend, herr = getHistoryStats(apiUrl, token, itemid, histType, 30)
				if herr != nil {
					log.Printf("[DEBUG] history.get also failed for process '%s' (itemid=%s): %v — marking disabled", name, itemid, herr)
					pr.Disabled = true
					pr.DisabledMsg = "Processo não habilitado"
					results <- procResult{idx: i, pr: pr}
					return
				}
				if trend == nil {
					pr.Disabled = true
					pr.DisabledMsg = "Processo não habilitado"
					results <- procResult{idx: i, pr: pr}
					return
				}
			}
			parseVal := func(k string) float64 {
				if v, ok := trend[k]; ok {
					s := fmt.Sprintf("%v", v)
					if f, err := strconv.ParseFloat(s, 64); err == nil { return f }
				}
				return -1
			}
			vmin := parseVal("value_min")
			vavg := parseVal("value_avg")
			vmax := parseVal("value_max")
			pr.Vmax = vmax
			pr.Vavg = vavg
			fmtVal := func(f float64) string {
				if f < 0 { return "-" }
				return fmt.Sprintf("%.2f%%", f)
			}
			pr.Smin = fmtVal(vmin)
			pr.Savg = fmtVal(vavg)
			pr.Smax = fmtVal(vmax)
			if vavg >= 0 {
				if vavg < 59.9 {
					pr.StatusText = "OK"
					pr.StatusStyle = "background:#66c28a;color:#000;padding:6px;border-radius:4px;text-align:center;"
				} else {
					pr.StatusText = "Atenção"
					pr.StatusStyle = "background:#ff6666;color:#000;padding:6px;border-radius:4px;text-align:center;"
				}
			} else {
				pr.StatusText = "-"
			}
			results <- procResult{idx: i, pr: pr}
		}()
	}
	pwg.Wait()
	close(results)
	// collect in original order
	tempMap := make(map[int]procRow)
	idxs := []int{}
	for r := range results { tempMap[r.idx] = r.pr; idxs = append(idxs, r.idx) }
	sort.Ints(idxs)
	for _, ii := range idxs { procRows = append(procRows, tempMap[ii]) }
	// sort by Vmax desc
	sort.Slice(procRows, func(i, j int) bool { return procRows[i].Vavg > procRows[j].Vavg })
	// render
	for _, pr := range procRows {
		nameCell := `<td style='position:relative;padding:0;'>` +
		`<div style='display:flex;align-items:center;gap:4px;'>` +
		`<span>` + pr.Friendly + `</span>` +
		`<span class='info-icon' tabindex='0' style='display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;cursor:pointer;outline:none;'>` +
		`<svg viewBox='0 0 16 16' width='14' height='14' style='display:block;'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='2' fill='white'/><text x='8' y='12' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
		`<span class='info-tooltip' style='display:none;position:absolute;z-index:10;left:22px;top:50%;transform:translateY(-50%);background:#e3f2fd;color:#102a43;padding:7px 12px;border-radius:6px;box-shadow:0 2px 8px rgba(0,0,0,0.08);font-size:13px;min-width:360px;max-width:720px;white-space:normal;overflow:visible;word-break:normal;overflow-wrap:break-word;'>` + htmlpkg.EscapeString(pr.Desc) + `</span>` +
		`</span>` +
		`</div></td>`
			// Adiciona JS/CSS para tooltip interrogação
			html += `<style>
			.info-icon:focus .info-tooltip,
			.info-icon:hover .info-tooltip {
				display: block;
			}
			.info-icon {
				outline: none;
			}
			\.info-tooltip {
							transition: opacity 0.15s;
							white-space: normal;
							overflow: visible;
							max-width: 520px;
							word-break: normal;
							overflow-wrap: break-word;
						}
			</style>
			<script>
			function setupInfoTooltips(){
			  document.querySelectorAll('.info-icon').forEach(function(icon){
				if(icon._tooltipBound) return;
				icon._tooltipBound = true;
				icon.addEventListener('click',function(e){
				  var tip = this.querySelector('.info-tooltip');
				  if(tip){ tip.style.display = (tip.style.display==='block') ? 'none' : 'block'; }
				  e.stopPropagation();
				});
			  });
			}
			setupInfoTooltips();
			document.addEventListener('click',function(){
			  document.querySelectorAll('.info-tooltip').forEach(function(tip){ tip.style.display='none'; });
			});
			// Se usar SPA ou renderização dinâmica, chame setupInfoTooltips() após atualizar a tabela
			</script>`
		if pr.Err {
			html += `<tr>` + nameCell + `<td colspan='4'>Erro ao obter dados</td></tr>`
			continue
		}
		if pr.Disabled {
			dm := "Processo não habilitado"
			if pr.DisabledMsg != "" { dm = pr.DisabledMsg }
			html += `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td style='background:#cccccc;color:#000;padding:6px;border-radius:4px;text-align:center;'>` + htmlpkg.EscapeString(dm) + `</td></tr>`
			continue
		}
		html += `<tr>` + nameCell + `<td>` + pr.Smin + `</td><td>` + pr.Savg + `</td><td>` + pr.Smax + `</td><td style='` + pr.StatusStyle + `'>` + pr.StatusText + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// close processos tab
	html += `</tbody></table></div>`
	html += `</div>` // end tab-processos

	// --- Proxys tab (Zabbix Proxys) ---
	html += `<div id='tab-proxys' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title'>Zabbix Proxys</h2>`
	html += titleWithInfo("h3", "Sumário Zabbix Proxys", "De preferencia para proxys Ativos. Proxys Passivos podem ser usados, em casos especificos, requer que o Zabbix Server consiga iniciar conexões com o Proxy. Verifique se os proxys estão atualizados e configurados corretamente.")
	// Small summary table for proxies (unknown / offline / active / passive / total) placed above details
	unknown := 0
	offline := 0
	active := 0
	passive := 0
	total := 0
    unknownNames := []string{}
    offlineNames := []string{}
	if len(proxies) > 0 {
		for _, p := range proxies {
			// Zabbix 7 retorna o campo 'state' (0=Unknown, 1=Offline, 2=Online).
			// Zabbix 6 não retorna 'state' — deriva o estado a partir de 'lastaccess'.
			stateRaw   := fmt.Sprintf("%v", p["state"])
			lastAccRaw := fmt.Sprintf("%v", p["lastaccess"])
			var effectiveState string
			if stateRaw != "" && stateRaw != "<nil>" {
				effectiveState = stateRaw
			} else {
				la, laErr := strconv.ParseInt(lastAccRaw, 10, 64)
				if laErr != nil || la == 0 {
					effectiveState = "0" // Unknown — nunca conectou
				} else if time.Now().Unix()-la > 300 {
					effectiveState = "1" // Offline — última conexão > 5 min atrás
				} else {
					effectiveState = "2" // Online
				}
			}
			proxyName := fmt.Sprintf("%v", p["name"])
			if proxyName == "<nil>" || proxyName == "" { proxyName = fmt.Sprintf("%v", p["host"]) }
			// count status-based categories (0=Unknown, 1=Offline)
			if effectiveState == "0" { unknown++; unknownNames = append(unknownNames, proxyName) }
			if effectiveState == "1" { offline++; offlineNames = append(offlineNames, proxyName) }

			// determine active/passive depending on Zabbix major version
			if majorV >= 7 {
				om := fmt.Sprintf("%v", p["operating_mode"])
				if om == "0" { active++ } else if om == "1" { passive++ }
			} else {
				// older Zabbix used different status codes for proxy running state
				st2 := fmt.Sprintf("%v", p["status"])
				if st2 == "5" { active++ } else if st2 == "6" { passive++ }
			}
		}
		total = len(proxies)
	} else {
		// if proxies not available, fall back to proxyCount for total
		total = proxyCount
	}
	// Renderiza tabela de sumario de proxys, e aplica estilos de destaque para categorias com contagem > 0 (vermelho para unknown/offline, verde para active, amarelo para passive)
	html += `<div class='table-responsive'><table class='modern-table'><colgroup><col style='width:75%'><col style='width:25%'></colgroup><thead><tr><th>Descrição</th><th>Quantidade</th></tr></thead><tbody>`
	unknownTdStyle := ""
	if unknown > 0 { unknownTdStyle = "background:#ff6666 !important;color:#000 !important;" }
	offlineTdStyle := ""
	if offline > 0 { offlineTdStyle = "background:#ff6666 !important;color:#000 !important;" }
	activeTdStyle := ""
	if active > 0 { activeTdStyle = "background:#66c28a !important;color:#000 !important;" }
	passiveTdStyle := ""
	if passive > 0 { passiveTdStyle = "background:#ffe08a !important;color:#000 !important;" }
	html += `<tr><td style='` + unknownTdStyle + `'>Proxys Unknown</td><td style='` + unknownTdStyle + `'>` + fmt.Sprintf("%d", unknown) + `</td></tr>`
	html += `<tr><td style='` + offlineTdStyle + `'>Proxys Offline</td><td style='` + offlineTdStyle + `'>` + fmt.Sprintf("%d", offline) + `</td></tr>`
	html += `<tr><td style='` + activeTdStyle + `'>Proxys Ativos</td><td style='` + activeTdStyle + `'>` + fmt.Sprintf("%d", active) + `</td></tr>`
	html += `<tr><td style='` + passiveTdStyle + `'>Proxys Passivos</td><td style='` + passiveTdStyle + `'>` + fmt.Sprintf("%d", passive) + `</td></tr>`
	html += `<tr><td>Total de Proxys</td><td>` + fmt.Sprintf("%d", total) + ` &nbsp; <a href='` + ambienteUrl + `/zabbix.php?action=proxy.list&filter_rst=1' target='_blank'>Abrir lista de Proxys</a></td></tr>`
	html += `</tbody></table></div>`

	// Proxys details table (list)
	// show all proxies in the details list, with Status column based on state
	if len(proxies) > 0 {
		html += `<h4>Proxys</h4>`
		html += `<div class='table-responsive'><table class='modern-table'><colgroup><col style='width:38%'><col style='width:10%'><col style='width:12%'><col style='width:12%'><col style='width:14%'><col style='width:14%'></colgroup><thead><tr><th>Proxy</th><th>Tipo</th><th>Total de Items</th><th>Items não suportados</th><th>Queue-10m</th><th>Status</th></tr></thead><tbody>`
			// parallelize per-proxy item calls to improve throughput
			type proxyRow struct{ idx int; html string }
			resultsP := make(chan proxyRow, len(proxies))
			var pwg sync.WaitGroup
			for i, p := range proxies {
				p := p
				i := i
				pwg.Add(1)
				go func() {
					defer pwg.Done()
					sem <- struct{}{}
					defer func(){ <-sem }()

					name := fmt.Sprintf("%v", p["name"])
					if name == "<nil>" || name == "" { name = fmt.Sprintf("%v", p["host"]) }
					proxyid := fmt.Sprintf("%v", p["proxyid"])
					tipo := "Desconhecido"
					if majorV >= 7 {
						om := fmt.Sprintf("%v", p["operating_mode"])
						if om == "0" { tipo = "Active" } else if om == "1" { tipo = "Passive" } else { tipo = om }
					} else {
						st := fmt.Sprintf("%v", p["status"])
						if st == "5" { tipo = "Active" } else if st == "6" { tipo = "Passive" } else { tipo = st }
					}

					queueVal := "-"
					itemsUnsupportedVal := "-"
					totalItemsVal := "-"

					paramsItems := map[string]interface{}{
						"search": map[string]interface{}{"key_": []string{
							"*items_unsupported*", "*configuration*syncer*", "*queue,10m*", "*data*sender*", "*availability*manager*",
							"*agent*poller*", "*browser*poller*", "*discovery*manager*", "*discovery*worker*", "*history*syncer*",
							"*housekeeper*", "*http*agent*poller*", "*http*poller*", "*icmp*pinger*", "*internal*poller*",
							"*ipmi*manager*", "*ipmi*poller*", "*java*poller*", "*odbc*poller*", "*poller*", "*preprocessing*manager*",
							"*preprocessing*worker*", "*self-monitoring*", "*snmp*poller*", "*snmp*trapper*", "*task*manager*",
							"*trapper*", "*unreachable*poller*", "*vmware*collector*",
						}},
						"searchWildcardsEnabled": true,
						"searchByAny": true,
						"monitored": true,
						"proxyids": proxyid,
						"output": "extend",
					}

					if respItems, ierr := zabbixApiRequest(apiUrl, token, "item.get", paramsItems); ierr == nil {
						if r, ok := respItems["result"]; ok {
							if arr, ok2 := r.([]interface{}); ok2 {
								for _, it := range arr {
									if m, mok := it.(map[string]interface{}); mok {
										key := fmt.Sprintf("%v", m["key_"])
										if strings.Contains(key, "queue,10m") || strings.HasPrefix(key, "zabbix[queue,10m") {
											if lv, lok := m["lastvalue"]; lok {
												queueVal = fmt.Sprintf("%v", lv)
											}
										}
										if strings.Contains(key, "items_unsupported") || key == "zabbix[items_unsupported]" {
											if lv, lok := m["lastvalue"]; lok {
												itemsUnsupportedVal = htmlpkg.EscapeString(fmt.Sprintf("%v", lv))
											}
										}
									}
								}
							}
						}
					} else {
						log.Printf("[DEBUG] item.get for proxy %s failed: %v", proxyid, ierr)
					}

					paramsTotal := map[string]interface{}{
						"output": "extend",
						"templated": false,
						"countOutput": true,
						"proxyids": proxyid,
					}
					if respTotal, terr := zabbixApiRequest(apiUrl, token, "item.get", paramsTotal); terr == nil {
						if r, ok := respTotal["result"]; ok {
							totalItemsVal = fmt.Sprintf("%v", r)
						}
					} else {
						log.Printf("[DEBUG] item.get (total) for proxy %s failed: %v", proxyid, terr)
					}

				// Status column: Zabbix 7 returns 'state' (0=Unknown,1=Offline,2=Online).
				// Zabbix 6 não retorna 'state' — deriva o estado a partir de 'lastaccess'.
				stateRaw2   := fmt.Sprintf("%v", p["state"])
				lastAccRaw2 := fmt.Sprintf("%v", p["lastaccess"])
				var effState string
				if stateRaw2 != "" && stateRaw2 != "<nil>" {
					effState = stateRaw2
				} else {
					la2, laErr2 := strconv.ParseInt(lastAccRaw2, 10, 64)
					if laErr2 != nil || la2 == 0 {
						effState = "0"
					} else if time.Now().Unix()-la2 > 300 {
						effState = "1"
					} else {
						effState = "2"
					}
				}
				var statusLabel, statusStyle string
				switch effState {
				case "2":
					statusLabel = "Online"
					statusStyle = "background:#66c28a;color:#000;padding:4px 8px;border-radius:4px;text-align:center;"
				case "1":
					statusLabel = "Offline"
					statusStyle = "background:#ff6666;color:#000;padding:4px 8px;border-radius:4px;text-align:center;"
				default: // 0 = Unknown
					statusLabel = "Unknown"
					statusStyle = "background:#ff6666;color:#000;padding:4px 8px;border-radius:4px;text-align:center;"
				}
				// Para proxies offline/desconhecidos, para coluna de State da tabela (nenhum dado coletado), mandraque para versão 6 e 7: mostra como offline/desconecido, e para as colunas de dados (queue, items unsupported, total items) mostra "-" para indicar que os dados não estão disponíveis devido ao estado do proxy. Para proxies online, mostra os valores reais.
				if effState != "2" {
					queueVal = "-"
					itemsUnsupportedVal = "-"
					totalItemsVal = "-"
				}
				// Proxy online mas chave não encontrada no template — exibe "-" com ícone de aviso no tooltip
				if effState == "2" && itemsUnsupportedVal == "-" {
					itemsUnsupportedVal = `-<span class='info-icon' tabindex='0' style='margin-left:4px;'>` +
						`<svg viewBox='0 0 16 16' width='14' height='14'><circle cx='8' cy='8' r='7' stroke='#e6a817' stroke-width='1.6' fill='white'/><text x='8' y='11' text-anchor='middle' font-size='10' fill='#e6a817' font-family='Arial' font-weight='bold'>!</text></svg>` +
						`<span class='info-tooltip'>Criar chave zabbix[items_unsupported] no Template de Proxy</span></span>`
				}

				rowHTML := `<tr data-proxyid='` + htmlpkg.EscapeString(proxyid) + `'><td>` + htmlpkg.EscapeString(name) + `</td><td>` + htmlpkg.EscapeString(tipo) + `</td><td style='text-align:center;'>` + htmlpkg.EscapeString(totalItemsVal) + `</td><td style='text-align:center;'>` + itemsUnsupportedVal + `</td><td style='text-align:center;'>` + htmlpkg.EscapeString(queueVal) + `</td><td style='` + statusStyle + `'>` + statusLabel + `</td></tr>`
				resultsP <- proxyRow{idx: i, html: rowHTML}
			}()
		}
		pwg.Wait()
		close(resultsP)
		// Continua com a ordem original dos proxys, incluo as cores
		rowsMap := make(map[int]string)
		idxs := []int{}
		for pr := range resultsP { rowsMap[pr.idx] = pr.html; idxs = append(idxs, pr.idx) }
		sort.Ints(idxs)
		for _, ii := range idxs { html += rowsMap[ii] }
		html += `</tbody></table></div>`
	} else {
		html += `<div class='como-corrigir'>Nenhum proxy configurado ou informação indisponível.</div>`
	}

	html += titleWithInfo("h3", "Processos e Threads Zabbix Proxys",
		`Os Zabbix Proxys possuem processos próprios de coleta e encaminhamento de dados. `+
		`As decisões de ajuste devem basear-se nas tendências dos últimos `+checkTrendDisplay+
		`: utilização média acima de 50% é sinal de atenção; acima de 60% recomenda-se aumentar o processo no arquivo zabbix_proxy.conf.`)
	if progressCb != nil { progressCb("Coletando processos e threads dos Zabbix Proxys...") }

	// All process names for proxy (pollers + internal merged into one table)
	proxyAllProcNames := []string{
		"data*sender",
		"poller",
		"unreachable*poller",
		"http*poller",
		"icmp*pinger",
		"ipmi*poller",
		"java*poller",
		"odbc*poller",
		"trapper",
		"preprocessing*manager",
		"preprocessing*worker",
		"configuration*syncer",
		"availability*manager",
		"discovery*manager",
		"discovery*worker",
		"history*syncer",
		"housekeeper",
		"ipmi*manager",
		"lld*manager",
		"lld*worker",
		"task*manager",
		"vmware*collector",
	}
	if majorV >= 7 {
		proxyAllProcNames = append([]string{"agent*poller", "browser*poller", "http*agent*poller", "snmp*poller"}, proxyAllProcNames...)
	}

	type proxyProcRow struct {
		friendly string
		vavg     float64
		rowHTML  string
	}
	type proxyProcResult struct {
		idx         int
		name        string
		online      bool
		rows        []proxyProcRow
		noItemsNote string
	}

	type proxyMetaP struct {
		Idx      int
		ProxyId  string
		Name     string
		Online   bool
		EffState string // "0"=Unknown, "1"=Offline, "2"=Online — mesma lógica da tabela Proxy
	}
	var proxyMetaList []proxyMetaP
	for i, p := range proxies {
		pid := fmt.Sprintf("%v", p["proxyid"])
		if pid == "" || pid == "<nil>" { continue }
		nm := fmt.Sprintf("%v", p["name"])
		if nm == "<nil>" || nm == "" { nm = fmt.Sprintf("%v", p["host"]) }
		// Reutiliza exatamente a mesma lógica de effState da tabela Proxy (não faz nova chamada à API):
		// Zabbix 7: campo 'state' retornado diretamente (0=Unknown, 1=Offline, 2=Online).
		// Zabbix 6: 'state' não existe — deriva de lastaccess com threshold de 300s.
		stateRaw   := fmt.Sprintf("%v", p["state"])
		lastAccRaw := fmt.Sprintf("%v", p["lastaccess"])
		var effState string
		if stateRaw != "" && stateRaw != "<nil>" {
			effState = stateRaw
		} else {
			la, laErr := strconv.ParseInt(lastAccRaw, 10, 64)
			if laErr != nil || la == 0 {
				effState = "0"
			} else if time.Now().Unix()-la > 300 {
				effState = "1"
			} else {
				effState = "2"
			}
		}
		proxyMetaList = append(proxyMetaList, proxyMetaP{Idx: i, ProxyId: pid, Name: nm, Online: effState == "2", EffState: effState})
	}

	ppCh := make(chan proxyProcResult, len(proxyMetaList)+1)
	var wgPP sync.WaitGroup
	for _, pm := range proxyMetaList {
		pm := pm
		wgPP.Add(1)
		go func() {
			defer wgPP.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := proxyProcResult{idx: pm.Idx, name: pm.Name, online: pm.Online}
			if !pm.Online {
				ppCh <- res
				return
			}

			// ── Step 1: discover the actual hostid of this proxy's self-monitoring host ──
			// Zabbix 6: proxyid == hostid  |  Zabbix 7: they differ → try multiple lookups
			hostId := ""
			// Attempt A: Zabbix 6 — proxyid IS the hostid
			if hckResp, hckErr := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{
				"output":  []string{"hostid"},
				"hostids": []string{pm.ProxyId},
			}); hckErr == nil {
				if rr, ok := hckResp["result"]; ok {
					if arr, ok2 := rr.([]interface{}); ok2 && len(arr) > 0 {
						hostId = pm.ProxyId
						log.Printf("[DEBUG] proxy '%s': Zabbix 6 path — proxyid=%s == hostid", pm.Name, hostId)
					}
				}
			}
			// Attempt B: Zabbix 7 — match by technical hostname (host field)
			if hostId == "" {
				if hnResp, hnErr := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{
					"output": []string{"hostid"},
					"filter": map[string]interface{}{"host": pm.Name},
				}); hnErr == nil {
					if rr, ok := hnResp["result"]; ok {
						if arr, ok2 := rr.([]interface{}); ok2 && len(arr) > 0 {
							if hm, ok3 := arr[0].(map[string]interface{}); ok3 {
								hostId = fmt.Sprintf("%v", hm["hostid"])
								log.Printf("[DEBUG] proxy '%s': found by technical name → hostid=%s", pm.Name, hostId)
							}
						}
					}
				}
			}
			// Attempt C: Zabbix 7 — match by display name (name field, exact)
			if hostId == "" {
				if hnResp, hnErr := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{
					"output": []string{"hostid"},
					"filter": map[string]interface{}{"name": pm.Name},
				}); hnErr == nil {
					if rr, ok := hnResp["result"]; ok {
						if arr, ok2 := rr.([]interface{}); ok2 && len(arr) > 0 {
							if hm, ok3 := arr[0].(map[string]interface{}); ok3 {
								hostId = fmt.Sprintf("%v", hm["hostid"])
								log.Printf("[DEBUG] proxy '%s': found by display name → hostid=%s", pm.Name, hostId)
							}
						}
					}
				}
			}
			if hostId == "" {
				hostId = pm.ProxyId
				log.Printf("[WARN] proxy '%s': hostid not resolved, using proxyid=%s as last resort", pm.Name, hostId)
			}

			// ── Step 2: single item.get for all process names on this proxy's host ──
			// Uses getProxyProcessItems: fetches ALL type=5 (Zabbix internal) items and
			// matches client-side on BOTH key_ AND name — robust across Zabbix 6/7 key formats.
			itemsMap, iErr := getProxyProcessItems(apiUrl, token, proxyAllProcNames, hostId)
			if iErr != nil {
				log.Printf("[ERROR] proxy '%s' item.get failed: %v", pm.Name, iErr)
				res.noItemsNote = fmt.Sprintf("Erro ao consultar itens (hostid=%s).", hostId)
				ppCh <- res
				return
			}
			if len(itemsMap) == 0 {
				res.noItemsNote = fmt.Sprintf("Nenhum item de processo encontrado. Verifique se o Host Existe ou se o Template \"Zabbix Proxy Health\" está vinculado ao host.", hostId)
				ppCh <- res
				return
			}

			// ── Step 3: collect itemids + value_types ──
			iids := make([]string, 0, len(itemsMap))
			vtypes := map[string]int{}
			for _, item := range itemsMap {
				iid := fmt.Sprintf("%v", item["itemid"])
				vt := 0
				if fmt.Sprintf("%v", item["value_type"]) == "3" { vt = 3 }
				iids = append(iids, iid)
				vtypes[iid] = vt
			}

			// ── Step 4: single trend.get for all items of this proxy (CHECKTRENDTIME respected) ──
			trendMap, _ := getTrendsBulkStats(apiUrl, token, iids)
			if trendMap == nil {
				trendMap = map[string]map[string]interface{}{}
			}

			// ── Step 5: history.get fallback for items without trend data ──
			missingH := map[string]int{}
			for _, iid := range iids {
				if _, ok := trendMap[iid]; !ok { missingH[iid] = vtypes[iid] }
			}
			if len(missingH) > 0 {
				histStats, _ := getHistoryStatsBulkByType(apiUrl, token, missingH)
				for iid, stats := range histStats {
					if _, exists := trendMap[iid]; !exists { trendMap[iid] = stats }
				}
			}

			// ── Step 6: build rows ──
			parseStat := func(tr map[string]interface{}) (smin, savg, smax string, vavg float64, stText, stStyle string) {
				pv := func(k string) float64 {
					if v, ok := tr[k]; ok {
						if f, e := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); e == nil { return f }
					}
					return -1
				}
				fv := func(f float64) string {
					if f < 0 { return "-" }
					return fmt.Sprintf("%.2f%%", f)
				}
				vavg = pv("value_avg")
				smin, savg, smax = fv(pv("value_min")), fv(vavg), fv(pv("value_max"))
				if vavg >= 0 {
					if vavg < 59.9 {
						stText = "OK"; stStyle = "background:#66c28a;color:#000;padding:4px 6px;border-radius:4px;text-align:center;"
					} else {
						stText = "Atenção"; stStyle = "background:#ff6666;color:#000;padding:4px 6px;border-radius:4px;text-align:center;"
					}
				} else {
					stText = "-"; stStyle = ""
				}
				return
			}
			for _, procName := range proxyAllProcNames {
				baseName := strings.ToLower(strings.TrimSpace(procName)) // raw key used for itemsMap lookup
				displayName := strings.ReplaceAll(procName, "*", " ")   // spaces for display and procDesc
				dispBaseName := strings.ToLower(strings.TrimSpace(displayName))
				// skip v7-only on v6
				if majorV < 7 {
					switch dispBaseName {
					case "agent poller", "browser poller", "http agent poller", "snmp poller", "configuration syncer worker":
						continue
					}
				}
				words := strings.Fields(displayName)
				for i, w := range words { tw := strings.TrimSpace(w); if len(tw) > 0 { words[i] = strings.ToUpper(tw[:1]) + strings.ToLower(tw[1:]) } }
				if len(words) > 0 && strings.ToLower(words[0]) == "lld" { words[0] = "LLD" }
				friendly := strings.Join(words, " ")
				desc := procDesc[dispBaseName]
				if desc == "" { desc = "Process" }

				nameCell := `<td style='position:relative;padding:0;'>` +
					`<div style='display:flex;align-items:center;gap:4px;'><span>` + htmlpkg.EscapeString(friendly) + `</span>` +
					`<span class='info-icon' tabindex='0' style='display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;cursor:pointer;outline:none;'>` +
					`<svg viewBox='0 0 16 16' width='14' height='14' style='display:block;'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='2' fill='white'/><text x='8' y='12' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
					`<span class='info-tooltip' style='display:none;position:absolute;z-index:10;left:22px;top:50%;transform:translateY(-50%);background:#e3f2fd;color:#102a43;padding:7px 12px;border-radius:6px;box-shadow:0 2px 8px rgba(0,0,0,0.08);font-size:13px;min-width:300px;max-width:640px;white-space:normal;word-break:normal;overflow-wrap:break-word;'>` +
					htmlpkg.EscapeString(desc) + `</span></span></div></td>`

				item := itemsMap[baseName]
				if item == nil {
					res.rows = append(res.rows, proxyProcRow{friendly: friendly, vavg: -1,
						rowHTML: `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td style='background:#cccccc;color:#000;padding:4px 6px;border-radius:4px;text-align:center;'>Processo não habilitado</td></tr>`})
					continue
				}
				iid := fmt.Sprintf("%v", item["itemid"])
				tr := trendMap[iid]
				if tr == nil {
					res.rows = append(res.rows, proxyProcRow{friendly: friendly, vavg: -1,
						rowHTML: `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td style='background:#cccccc;color:#000;padding:4px 6px;border-radius:4px;text-align:center;'>Sem dados</td></tr>`})
					continue
				}
				smin, savg, smax, vavg, stText, stStyle := parseStat(tr)
				res.rows = append(res.rows, proxyProcRow{friendly: friendly, vavg: vavg,
					rowHTML: `<tr>` + nameCell + `<td>` + smin + `</td><td>` + savg + `</td><td>` + smax + `</td><td style='` + stStyle + `'>` + stText + `</td></tr>`})
			}
			// sort by vavg desc (busiest first)
			sort.Slice(res.rows, func(i, j int) bool { return res.rows[i].vavg > res.rows[j].vavg })
			ppCh <- res
		}()
	}
	wgPP.Wait()
	close(ppCh)

	ppResults := map[int]proxyProcResult{}
	for r := range ppCh { ppResults[r.idx] = r }

	// Build list of proxy processes in Atenção for the Recommendations section
	type proxyProcAttnItem struct{ ProxyName, ProcFriendly string; Vavg float64 }
	var proxyProcAttnList []proxyProcAttnItem
	// Proxies online mas sem itens de processo (template não vinculado)
	type proxyNoTemplateItem struct{ ProxyName, HostId string }
	var proxyNoTemplateList []proxyNoTemplateItem
	for _, pm := range proxyMetaList {
		if !pm.Online { continue }
		res := ppResults[pm.Idx]
		if res.noItemsNote != "" {
			proxyNoTemplateList = append(proxyNoTemplateList, proxyNoTemplateItem{ProxyName: pm.Name, HostId: pm.ProxyId})
		}
		for _, row := range res.rows {
			if row.vavg >= 59.9 {
				proxyProcAttnList = append(proxyProcAttnList, proxyProcAttnItem{
					ProxyName:   pm.Name,
					ProcFriendly: row.friendly,
					Vavg:        row.vavg,
				})
			}
		}
	}

	if len(proxyMetaList) == 0 {
		html += `<div class='como-corrigir'>Nenhum proxy configurado.</div>`
	} else {
		html += `<div style='display:flex;flex-direction:column;gap:10px;margin-top:8px;'>`
		for _, pm := range proxyMetaList {
			res := ppResults[pm.Idx]
			// Online/Offline/Unknown badge — reutiliza pm.EffState calculado com a mesma lógica da tabela Proxy (sem nova chamada à API)
			var connBadge string
			switch pm.EffState {
			case "2":
				connBadge = `<span style='margin-left:8px;background:#66c28a;color:#000;padding:2px 8px;border-radius:4px;font-size:0.82em;'>Online</span>`
			case "1":
				connBadge = `<span style='margin-left:8px;background:#ff6666;color:#000;padding:2px 8px;border-radius:4px;font-size:0.82em;'>Offline</span>`
			default:
				connBadge = `<span style='margin-left:8px;background:#aaaaaa;color:#000;padding:2px 8px;border-radius:4px;font-size:0.82em;'>Unknown</span>`
			}
			// Status badge (OK / Atenção) — computed from worst vavg across all rows
			statusBadge := ``
			if pm.Online {
				hasData := false
				hasAtencao := false
				for _, row := range res.rows {
					if row.vavg >= 0 {
						hasData = true
						if row.vavg >= 59.9 { hasAtencao = true }
					}
				}
				if hasData {
					if hasAtencao {
						statusBadge = `<span style='margin-left:6px;background:#ff6666;color:#000;padding:2px 8px;border-radius:4px;font-size:0.82em;'>Atenção</span>`
					} else {
						statusBadge = `<span style='margin-left:6px;background:#66c28a;color:#000;padding:2px 8px;border-radius:4px;font-size:0.82em;'>OK</span>`
					}
				}
			}
			html += `<details style='border:1px solid #d1d5db;border-radius:6px;padding:0;'>`
			html += `<summary style='padding:10px 14px;cursor:pointer;font-weight:600;font-size:0.95em;list-style:none;display:flex;align-items:center;'>` +
				`<svg viewBox='0 0 10 10' width='10' height='10' style='margin-right:6px;flex-shrink:0;'><path d='M2 3 L5 7 L8 3' stroke='#555' stroke-width='1.5' fill='none'/></svg>` +
				htmlpkg.EscapeString(pm.Name) + connBadge + statusBadge + `</summary>`
			if !pm.Online {
				html += `<div style='padding:10px 14px;color:#888;'>Proxy offline ou unknown — dados de processos não disponíveis.</div></details>`
				continue
			}
			html += `<div style='padding:8px 14px 12px;'>`
			if res.noItemsNote != "" {
				html += `<div class='como-corrigir' style='margin:4px 0 8px;'>` + htmlpkg.EscapeString(res.noItemsNote) + `</div>`
			}
			if len(res.rows) > 0 {
				html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Processo</th><th>value_min</th><th>value_avg</th><th>value_max</th><th>Status</th></tr></thead><tbody>`
				for _, r := range res.rows { html += r.rowHTML }
				html += `</tbody></table></div>`
			}
			html += `</div></details>`
		}
		html += `</div>`
	}

	html += `</div>` // end tab-proxys

	// --- Items tab (Items não suportados + Intervalo de Coleta) ---
	if progressCb != nil { progressCb("Coletando informações de Items sem Template e Items não suportados...") }
	html += `<div id='tab-items' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title'>Items e LLDs</h2>`
	// --- Items não suportados (nova categoria) ---
	// Choose the frontend path depending on Zabbix major version (>=7 use zabbix.php, older use items.php)
	itemsPath := ""
	if majorV >= 7 {
		itemsPath = "zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1"
	} else {
		itemsPath = "items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1"
	}

	// --- Items sem Template (moved here: items created at host level, not from templates) ---
	itemsNoTplCount = 0
	paramsNoTpl := map[string]interface{}{
		"output": "extend",
		"filter": map[string]interface{}{"flags": 0},
		"countOutput": true,
		"templated": false,
		"inherited": false,
	}
	if respNoTpl, errNoTpl := zabbixApiRequest(apiUrl, token, "item.get", paramsNoTpl); errNoTpl == nil {
		if r, ok := respNoTpl["result"]; ok {
			switch v := r.(type) {
			case float64:
				itemsNoTplCount = int(v)
			case int:
				itemsNoTplCount = v
			default:
				if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { itemsNoTplCount = v2 }
			}
		}
	}
	// build link for items without template
	var itemsNoTplPath string
	if majorV >= 7 {
		itemsNoTplPath = "zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=0&filter_discovered=0&filter_set=1"
	} else {
		itemsNoTplPath = "items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=0&filter_discovered=0&filter_set=1"
	}
	itemsNoTplLink := ambienteUrl + "/" + itemsNoTplPath

	unsupportedLink := ambienteUrl + "/" + itemsPath
	html += titleWithInfo("h3", "Items sem Template", "Item sem template não afeta performance de Processos do Zabbix, porem para melhor organização é importante um item ter template e não ser criado a nivel de Host.")
	if itemsNoTplCount > 0 {
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Descrição</th><th>Quantidade</th><th>Link</th></tr></thead><tbody>`
		html += `<tr><td>Items sem Template</td><td>` + fmt.Sprintf("%d", itemsNoTplCount) + `</td><td><a href='` + itemsNoTplLink + `' target='_blank'>Abrir</a></td></tr>`
		html += `</tbody></table></div>`
	} else {
		html += ``
	}

	html += titleWithInfo("h3", "Items não suportados", "Como corrigir: " + descNaoSuportados)
	// (legend use .como-corrigir)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Tipo de Item</th><th>Total</th><th>Não suportados</th><th>Link</th></tr></thead><tbody>`

	// Define item types to query (type code -> label)
	baseTypes := []struct{ Code int; Label string }{
		{0, "Zabbix Agent"},
		{2, "Zabbix Trapper"},
		{3, "Simple check"},
		{5, "Zabbix internal"},
		{7, "Zabbix agent (active)"},
		{8, "Aggregate"},
		{9, "Web item"},
		{10, "External check"},
		{11, "Database"},
		{12, "IPMI"},
		{13, "SSH"},
		{14, "Telnet"},
		{15, "Calculated"},
		{16, "JMX"},
		{17, "SNMP Trap"},
		{18, "Dependent item"},
		{19, "HTTP agent"},
		{20, "SNMP"},
		{21, "Script"},
	}
	types := baseTypes
	// Include Browser (22) only when Zabbix major version is 7 or newer
	if majorV >= 7 {
		types = append(types, struct{ Code int; Label string }{22, "Browser"})
	}

	// If Zabbix < 7, browser (22) is not supported in frontend listing; we skip showing it
	// collect rows so we can sort by unsupported count
	type rowT struct{ Label string; Total int; Unsup int; Link string }

	// use semaphore defined at function top to limit concurrency
	var wg sync.WaitGroup
	resultsCh := make(chan rowT, len(types))

	for _, t := range types {
		t := t
		wg.Add(1)
		go func(tt struct{ Code int; Label string }) {
			defer wg.Done()
			sem <- struct{}{}
			defer func(){ <-sem }()

			// build params for total count of this type
			paramsTotal := map[string]interface{}{
				"output": "extend",
				"filter": map[string]interface{}{ "type": tt.Code },
				"templated": false,
				"countOutput": true,
				"monitored": true,
			}
			// build params for unsupported count of this type (state = 1)
			paramsUnsup := map[string]interface{}{
				"output": "extend",
				"filter": map[string]interface{}{ "type": tt.Code, "state": 1 },
				"templated": false,
				"countOutput": true,
				"monitored": true,
			}

			cntTotal := 0
			cntUnsup := 0

			if respTotal, errTotal := zabbixApiRequest(apiUrl, token, "item.get", paramsTotal); errTotal == nil {
				if r, ok := respTotal["result"]; ok {
					switch v := r.(type) {
					case float64:
						cntTotal = int(v)
					case int:
						cntTotal = v
					default:
						if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { cntTotal = v2 }
					}
				}
			}
			if respUns, errUns := zabbixApiRequest(apiUrl, token, "item.get", paramsUnsup); errUns == nil {
				if r, ok := respUns["result"]; ok {
					switch v := r.(type) {
					case float64:
						cntUnsup = int(v)
					case int:
						cntUnsup = v
					default:
						if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { cntUnsup = v2 }
					}
				}
			}

			perPath := strings.Replace(itemsPath, "filter_type=-1", fmt.Sprintf("filter_type=%d", tt.Code), 1)
			perLink := ambienteUrl + "/" + perPath
			linkHTML := ""
			if tt.Code == 22 && majorV < 7 {
				linkHTML = "-"
			} else {
				linkHTML = "<a href='" + perLink + "' target='_blank'>Abrir</a>"
			}

			resultsCh <- rowT{Label: tt.Label, Total: cntTotal, Unsup: cntUnsup, Link: linkHTML}
		}(t)
	}

	wg.Wait()
	close(resultsCh)

	rows := []rowT{}
	for r := range resultsCh { rows = append(rows, r) }

	// sort by Unsup desc
	sort.Slice(rows, func(i, j int) bool { return rows[i].Unsup > rows[j].Unsup })

	for _, r := range rows {
		if r.Unsup == 0 {
			continue
		}
		html += `<tr><td>` + r.Label + `</td><td>` + fmt.Sprintf("%d", r.Total) + `</td><td>` + fmt.Sprintf("%d", r.Unsup) + `</td><td>` + r.Link + `</td></tr>`
	}

	// final total row: use existing total items query result for total items, and unsupportedVal for not supported total
	totalItemsInt := 0
	if nItemsTotal != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItemsTotal)); err == nil { totalItemsInt = v }
	}
	html += `<tr><td><strong>Total</strong></td><td><strong>` + fmt.Sprintf("%d", totalItemsInt) + `</strong></td><td><strong>` + fmt.Sprintf("%d", unsupportedVal) + `</strong></td><td><a href='` + unsupportedLink + `' target='_blank'>Abrir listagem completa</a></td></tr>`
	html += `</tbody></table></div>`

	// --- Intervalo de Coleta ---
	// Coleta a quantidade de itens com update interval de 1s, 10s, 30s e 60s
	intervals := []int{1, 10, 30, 60}
	intervalRows := []struct{ Interval int; Count int; Link string }{}
	for _, d := range intervals {
		params := map[string]interface{}{
			"output": "extend",
			"filter": map[string]interface{}{"delay": d},
			"templated": false,
			"countOutput": true,
		}
		cnt := 0
		if resp, err := zabbixApiRequest(apiUrl, token, "item.get", params); err == nil {
			if r, ok := resp["result"]; ok {
				switch v := r.(type) {
				case float64:
					cnt = int(v)
				case int:
					cnt = v
				default:
					if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { cnt = v2 }
				}
			}
		}
		// montar link para a listagem com filter_delay (usar path apropriado para versão do Zabbix)
		var perPath string
		if majorV >= 7 {
			perPath = fmt.Sprintf("zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=%d&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_state=-1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1", d)
		} else {
			perPath = fmt.Sprintf("items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=%d&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1", d)
		}
		perLink := ambienteUrl + "/" + perPath
		linkHTML := "<a href='" + perLink + "' target='_blank'>Abrir</a>"
		intervalRows = append(intervalRows, struct{ Interval int; Count int; Link string }{Interval: d, Count: cnt, Link: linkHTML})
	}

	// renderiza a seção de Intervalo de Coleta
	// (legend use .como-corrigir)
	html += titleWithInfo("h3", "Intervalo de Coleta:", "As métricas de monitoramento serão coletadas com base no intervalo de coleta definido no item, quanto menor o intervalo de coleta mais recursos de CPU e memória será utilizado no Zabbix Server e/ou Zabbix Proxy além de relação direta com o crescimento do Banco de Dados, VPS do Zabbix e no processo de Housekeeper. Intervalos Verificados 1, 10, 30, 60.")
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Intervalo (s)</th><th>Quantidade de itens</th><th>Link</th></tr></thead><tbody>`
	for _, r := range intervalRows {
		if r.Count == 0 {
			continue
		}
		html += `<tr><td>` + fmt.Sprintf("%d", r.Interval) + `</td><td>` + fmt.Sprintf("%d", r.Count) + `</td><td>` + r.Link + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// --- Regras de LLD (Discovery rules) ---
	// Coleta a quantidade de regras de descoberta (discoveryrule) com delay 1s,10s,30s,60s,300s
	lldIntervals := []int{1, 10, 30, 60, 300}
	lldRows := []struct{ Interval int; Count int; Link string }{}
	for _, d := range lldIntervals {
		params := map[string]interface{}{
			"output": "extend",
			"filter": map[string]interface{}{"delay": d},
			"templated": true,
			"countOutput": true,
		}
		cnt := 0
		if resp, err := zabbixApiRequest(apiUrl, token, "discoveryrule.get", params); err == nil {
			if r, ok := resp["result"]; ok {
				switch v := r.(type) {
				case float64:
					cnt = int(v)
				case int:
					cnt = v
				default:
					if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { cnt = v2 }
				}
			}
		}

		// montar link para a listagem de discovery rules com filter_delay (usar path apropriado para versão do Zabbix)
		// formatar delay como "Ns" ou "Nm" para o link (Zabbix frontend espera p.ex. 5m)
		delayFmt := ""
		if d >= 60 && d%60 == 0 {
			delayFmt = fmt.Sprintf("%dm", d/60)
		} else {
			delayFmt = fmt.Sprintf("%ds", d)
		}

		var perPath string
		if majorV >= 7 {
			perPath = fmt.Sprintf("host_discovery.php?context=template&filter_name=&filter_key=&filter_type=-1&filter_delay=%s&filter_lifetime_type=-1&filter_enabled_lifetime_type=-1&filter_snmp_oid=&filter_status=-1&filter_set=1", delayFmt)
		} else {
			perPath = fmt.Sprintf("host_discovery.php?context=template&filter_name=&filter_key=&filter_type=-1&filter_delay=%s&filter_lifetime=&filter_snmp_oid=&filter_status=-1&filter_set=1", delayFmt)
		}
		perLink := ambienteUrl + "/" + perPath
		linkHTML := "<a href='" + perLink + "' target='_blank'>Abrir</a>"

		lldRows = append(lldRows, struct{ Interval int; Count int; Link string }{Interval: d, Count: cnt, Link: linkHTML})
	}

	// renderiza a seção de Regras de LLD (Intervalo de Coleta)
	html += titleWithInfo("h3", "Regras de LLD (Discovery rules) - Intervalo de Coleta:", "As métricas de monitoramento serão coletadas com base no intervalo de coleta definido na regra LLD; quanto menor o intervalo, maior o consumo de CPU/memória e crescimento do banco. Verifique se intervalos muito curtos são realmente necessários.")
	// legend moved into tooltip via titleWithInfo
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Intervalo (s)</th><th>Quantidade de regras</th><th>Link</th></tr></thead><tbody>`
	for _, r := range lldRows {
		if r.Count == 0 {
			continue
		}
		html += `<tr><td>` + fmt.Sprintf("%d", r.Interval) + `</td><td>` + fmt.Sprintf("%d", r.Count) + `</td><td>` + r.Link + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// --- Regras de LLD (Discovery rules) - Not Supported ---
	// Conta discovery rules com state = 1 (Not supported / erro) no escopo de hosts (não templated)
	lldNotSupCnt := 0
	paramsNotSup := map[string]interface{}{
		"output": "extend",
		"filter": map[string]interface{}{"state": 1},
		"templated": false,
		"countOutput": true,
	}
	if resp, err := zabbixApiRequest(apiUrl, token, "discoveryrule.get", paramsNotSup); err == nil {
		if r, ok := resp["result"]; ok {
			switch v := r.(type) {
			case float64:
				lldNotSupCnt = int(v)
			case int:
				lldNotSupCnt = v
			default:
				if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { lldNotSupCnt = v2 }
			}
		}
	}

	// montar link para a listagem de discovery rules com filter_state=1 (usar path apropriado para versão do Zabbix)
	var lldPerPath string
	if majorV >= 7 {
		lldPerPath = "host_discovery.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_delay=&filter_lifetime_type=-1&filter_enabled_lifetime_type=-1&filter_snmp_oid=&filter_state=1&filter_set=1"
	} else {
		lldPerPath = "host_discovery.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_delay=&filter_lifetime=&filter_snmp_oid=&filter_state=1&filter_set=1"
	}
	lldPerLink := ambienteUrl + "/" + lldPerPath

	if lldNotSupCnt > 0 {
		html += titleWithInfo("h3", "Regras de LLD (Discovery rules) - Not Supported", "Regras não suportadas são aquelas que apresentam erro ao executar a coleta. Essas regras continuam consumindo processos do Zabbix desnecessariamente; valide os erros e corrija ou desative regras inválidas.")
		// legend moved into tooltip via titleWithInfo
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Descrição</th><th>Quantidade</th><th>Link</th></tr></thead><tbody>`
		html += `<tr><td>Regras de descoberta com status de erro</td><td>` + fmt.Sprintf("%d", lldNotSupCnt) + `</td><td><a href='` + lldPerLink + `' target='_blank'>Abrir</a></td></tr>`
		html += `</tbody></table></div>`
	}

	// --- Items Texto com Histórico ---
	// Busca items do tipo Texto (value_type = 4) com intervalo de coleta menor ou igual a 300s
	html += titleWithInfo("h3", "Items Texto com Historico", "Items do tipo Texto, tem um custo elevado espaço em disco em Banco de Dados, com intervalo de checagem baixo, há muita retenção de informação. Esta coleta verifica items do tipo Texto, com History(1h, 1d, 7d ou 31d) e Intervalo de Coleta menor que 5m (não há validade de preprocessamento).")

	// request host linkage for items so we can fetch templates in one call
	paramsTextItems := map[string]interface{}{
		"output":      []string{"name", "itemid", "delay"},
		"templated":   true,
		"filter": map[string]interface{}{
			"value_type": 4,
			"delay":      []interface{}{30, 60, 120, 300},
			"history":    []interface{}{"1h", "1d", "7d", "31d"},
		},
		"sortfield":  "name",
		// include hosts info for each item
		"selectHosts": []string{"hostid"},
	}

	type textRowT struct{ Template string; Name string; ItemID string; Delay string; HostID string }
	textRows := []textRowT{}
	textCount := 0
	hostIDSet := map[string]bool{}
	if respText, errText := zabbixApiRequest(apiUrl, token, "item.get", paramsTextItems); errText == nil {
		if r, ok := respText["result"]; ok {
			if arr, ok2 := r.([]interface{}); ok2 {
				for _, it := range arr {
					if m, okm := it.(map[string]interface{}); okm {
						name := fmt.Sprintf("%v", m["name"])
						itemid := fmt.Sprintf("%v", m["itemid"])
						delay := fmt.Sprintf("%v", m["delay"])
						if delay == "" { delay = "-" }

						hostid := ""
						if hostsRaw, okh := m["hosts"]; okh {
							if hostsArr, okha := hostsRaw.([]interface{}); okha && len(hostsArr) > 0 {
								if h0, okh0 := hostsArr[0].(map[string]interface{}); okh0 {
									hostid = fmt.Sprintf("%v", h0["hostid"])
								}
							}
						}
						if hostid != "" { hostIDSet[hostid] = true }

						textRows = append(textRows, textRowT{Template: "", Name: name, ItemID: itemid, Delay: delay, HostID: hostid})
						textCount++
					}
				}
			}
		}
	}

	// Busca template por hostid para mapear quais templates
	hostIDs := []interface{}{}
	for hid := range hostIDSet { hostIDs = append(hostIDs, hid) }
	hostToTemplates := map[string][]string{}
	templateIDToName := map[string]string{}
	if len(hostIDs) > 0 {
		paramsTpl := map[string]interface{}{
			"output": []string{"templateid", "name"},
			"filter": map[string]interface{}{"hostid": hostIDs},
			// include hosts so we can map which template belongs to which host
			"selectHosts": []string{"hostid"},
		}
		if respTpl, errTpl := zabbixApiRequest(apiUrl, token, "template.get", paramsTpl); errTpl == nil {
			if rtpl, ok := respTpl["result"]; ok {
				if arrTpl, ok2 := rtpl.([]interface{}); ok2 {
					for _, tt := range arrTpl {
						if tm, okm := tt.(map[string]interface{}); okm {
							tname := fmt.Sprintf("%v", tm["name"])
							tid := fmt.Sprintf("%v", tm["templateid"])
							if tid != "" {
								templateIDToName[tid] = tname
							}
							if hostsRaw, okh := tm["hosts"]; okh {
								if hostsArr, okha := hostsRaw.([]interface{}); okha {
									for _, hr := range hostsArr {
										if hm, okhm := hr.(map[string]interface{}); okhm {
											hid := fmt.Sprintf("%v", hm["hostid"])
											hostToTemplates[hid] = append(hostToTemplates[hid], tname)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if textCount > 0 {
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Template</th><th>Nome do Item</th><th>ItemID</th><th>Intervalo de Checagem (s)</th><th>Link</th></tr></thead><tbody>`
		for _, tr := range textRows {
			tplNames := hostToTemplates[tr.HostID]
			tplCell := "-"
			tplID := ""
			if len(tplNames) > 0 {
				tplCell = htmlpkg.EscapeString(strings.Join(tplNames, ", "))
				// try to find templateid by matching name
				for id, nm := range templateIDToName {
					for _, tn := range tplNames {
						if nm == tn {
							tplID = id
							break
						}
					}
					if tplID != "" { break }
				}
			} else {
				// fallback: if templateid equals hostid, try templateIDToName
				if name, ok := templateIDToName[tr.HostID]; ok {
					tplCell = htmlpkg.EscapeString(name)
					tplID = tr.HostID
				}
			}

			// build link to open item list in frontend (filter_hostids = templateid, filter_name = item name)
			linkHTML := "-"
			if tplID != "" {
				escName := neturl.QueryEscape(tr.Name)
				if majorV >= 7 {
					perPath := fmt.Sprintf("zabbix.php?action=item.list&context=template&filter_hostids%%5B%%5D=%s&filter_name=%s&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_status=-1&filter_with_triggers=-1&filter_inherited=-1&filter_set=1", tplID, escName)
					linkHTML = "<a href='" + ambienteUrl + "/" + perPath + "' target='_blank'>Abrir</a>"
				} else {
					perPath := fmt.Sprintf("items.php?context=template&filter_hostids%%5B%%5D=%s&filter_name=%s&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_status=-1&filter_with_triggers=-1&filter_inherited=-1&filter_set=1", tplID, escName)
					linkHTML = "<a href='" + ambienteUrl + "/" + perPath + "' target='_blank'>Abrir</a>"
				}
			}

			html += `<tr><td>` + tplCell + `</td><td>` + htmlpkg.EscapeString(tr.Name) + `</td><td>` + tr.ItemID + `</td><td>` + tr.Delay + `</td><td>` + linkHTML + `</td></tr>`
		}
		html += `</tbody></table></div>`
	}

	// close items tab
	html += `</div>` // end tab-items

	// --- Templates tab ---
	html += `<div id='tab-templates' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title'>Templates</h2>`
	// Detalhamento dos Principais Templates
	html += titleWithInfo("h3", "Erros mais comuns dos Principais Templates Ofendores", descDetalhamento)
		// legend moved into tooltip via titleWithInfo
	for _, tpl := range topTemplates {
		tplName := templateNames[tpl.Key]
		if tplName == "" { tplName = tpl.Key }
		html += `<h4>` + tplName + `</h4>`
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Item</th><th>Erro</th><th>Host</th><th>Link</th></tr></thead><tbody>`
		rows := templateItems[tpl.Key]
		for i, row := range rows {
			if i >= topN { break }
			html += `<tr><td>` + row[0] + `</td><td>` + row[1] + `</td><td>` + row[2] + `</td><td><a href='` + row[3] + `' target='_blank'>Editar</a></td></tr>`
		}
		html += `</tbody></table></div>`
	}
	// close templates tab and main container
	html += `</div>` // end tab-templates

	// --- Top Templates/Items tab ---
	html += `<div id='tab-top' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title'>Top Hosts/Templates/Items</h2>`
	// Top Templates Ofensores
	html += titleWithInfo("h3", "Top Templates Ofensores", "Como corrigir: " + descTemplates)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Template</th><th>Quantidade de Erros</th></tr></thead><tbody>`
	for _, tpl := range topTemplates {
		tplName := templateNames[tpl.Key]
		if tplName == "" { tplName = tpl.Key }
		html += `<tr><td>` + tplName + `</td><td>` + fmt.Sprintf("%d", tpl.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Montar o Top Hosts Ofensores (com template mais recorrente com itens problemáticos para cada host)
	html += titleWithInfo("h3", "Top Hosts Ofensores", "Como corrigir: " + descHosts)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Host</th><th>Template Mais Ofensor</th><th>Quantidade de Erros</th></tr></thead><tbody>`
	for _, host := range topHosts {
		// Descobrir o template mais recorrente para o host
		tplCount := map[string]int{}
		for _, row := range hostItems[host.Key] {
			tplCount[row[2]]++
		}
		mainTplId := ""
		maxCount := 0
		for tplId, count := range tplCount {
			if count > maxCount { mainTplId = tplId; maxCount = count }
		}
		mainTplName := templateNames[mainTplId]
		if mainTplName == "" { mainTplName = mainTplId }
		html += `<tr><td>` + host.Key + `</td><td>` + mainTplName + `</td><td>` + fmt.Sprintf("%d", host.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Top Items Problemáticos
	html += titleWithInfo("h3", "Top Items Problemáticos", "Como corrigir: " + descItems)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Item</th><th>Template</th><th>Quantidade de Erros</th></tr></thead><tbody>`
	for _, item := range topItems {
		parts := strings.SplitN(item.Key, "|", 2)
		itemName := parts[0]
		tplId := ""
		if len(parts) > 1 { tplId = parts[1] }
		tplName := templateNames[tplId]
		if tplName == "" { tplName = tplId }
		html += `<tr><td>` + itemName + `</td><td>` + tplName + `</td><td>` + fmt.Sprintf("%d", item.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Tipos de Erro Mais Comuns
	html += titleWithInfo("h3", "Tipos de Erro Mais Comuns", "Como corrigir: " + descErros)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Mensagem de Erro</th><th>Template</th><th>Ocorrências</th></tr></thead><tbody>`
	for _, errRow := range topErrors {
		parts := strings.SplitN(errRow.Key, "|", 2)
		errMsg := parts[0]
		tplId := ""
		if len(parts) > 1 { tplId = parts[1] }
		tplName := templateNames[tplId]
		if tplName == "" { tplName = tplId }
		html += `<tr><td>` + errMsg + `</td><td>` + tplName + `</td><td>` + fmt.Sprintf("%d", errRow.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`
	html += `</div>` // end tab-top

	// Recomendações tab (espaço para sugestões automáticas / ações)
	html += `<div id='tab-recomendacoes' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title'>Recomendações</h2>`
	html += titleWithInfo("h3", "Recomendações", "Sugestões geradas automaticamente com base no relatório. Use como ponto de partida para investigações e correções.")

	// precompute aggregates needed by the KPI/cards (attention list, interval/LDD small-interval counts)
	attention := []struct{ Name string; Vavg float64 }{}
	for _, pr := range pollRows {
		if pr.StatusText == "Atenção" && pr.Vavg >= 0 {
			attention = append(attention, struct{ Name string; Vavg float64 }{pr.Friendly, pr.Vavg})
		}
	}
	for _, pr := range procRows {
		if pr.StatusText == "Atenção" && pr.Vavg >= 0 {
			attention = append(attention, struct{ Name string; Vavg float64 }{pr.Friendly, pr.Vavg})
		}
	}
	sort.Slice(attention, func(i, j int) bool { return attention[i].Vavg > attention[j].Vavg })

	// items with interval <= 60s (sum 1,10,30,60)
	itemsLe60 := 0
	for _, r := range intervalRows { if r.Interval <= 60 { itemsLe60 += r.Count } }
	// LLD rules with interval <= 300s (sum 1,10,30,60,300)
	lldLe300 := 0
	for _, r := range lldRows { if r.Interval <= 300 { lldLe300 += r.Count } }

	// get em itens SNMP com SNMP OID GET E WALK, utilzado no Zabbix 7,
	snmpTplCount := 0
	snmpGetWalkCount := 0
	if majorV >= 7 {
		if resp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
			"output": "extend",
			"templated": true,
			"countOutput": true,
			"filter": map[string]interface{}{"type": 20},
		}); err == nil {
			if r, ok := resp["result"]; ok {
				switch v := r.(type) {
				case float64:
					snmpTplCount = int(v)
				case string:
					if iv, ierr := strconv.Atoi(v); ierr == nil { snmpTplCount = iv }
				}
			}
		}
		if resp2, err2 := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
			"filter": map[string]interface{}{"type": 20},
			"search": map[string]interface{}{"snmp_oid": []string{"get[*", "walk[*"}},
			"searchWildcardsEnabled": true,
			"searchByAny": true,
			"countOutput": true,
			"templated": true,
		}); err2 == nil {
			if r2, ok2 := resp2["result"]; ok2 {
				switch v := r2.(type) {
				case float64:
					snmpGetWalkCount = int(v)
				case string:
					if iv, ierr := strconv.Atoi(v); ierr == nil { snmpGetWalkCount = iv }
				}
			}
		}
	}

// Busca itens SNMP em Templates que AINDA NÃO estão usando OIDs get[]/walk[], para montar a recomencaçao de migração de SNMP para versões mais modernas do Zabbix (que utilizam get[]/walk[] e não dependem do formato antigo de OID). A ideia é identificar quais templates SNMP ainda possuem itens usando o formato antigo (sem get[]/walk[]) e listar esses templates como candidatos à migração, já que eles provavelmente estão utilizando o método de coleta SNMP mais antigo e menos eficiente, para migrar para o poller assincrono.
// Coletamos seus IDs de host, resolvemos os nomes dos Templates e removemos duplicatas.
	snmpMigrationTpls := []string{} // sorted list of template names
	if majorV >= 7 {
		if respSnmpAll, errSnmpAll := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
			"output":    []string{"itemid", "hostid"},
			"filter":    map[string]interface{}{"type": 20},
			"templated": true,
			"selectHosts": []string{"hostid"},
		}); errSnmpAll == nil {
			if respSnmpGW, errSnmpGW := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
				"output":                []string{"itemid", "hostid"},
				"filter":                map[string]interface{}{"type": 20},
				"search":                map[string]interface{}{"snmp_oid": []string{"get[*", "walk[*"}},
				"searchWildcardsEnabled": true,
				"searchByAny":           true,
				"templated":             true,
				"selectHosts": []string{"hostid"},
			}); errSnmpGW == nil {
				// Pesquisa e constrói o conjunto de hostids que já utilizam get[]/walk[]
				modernHostids := map[string]struct{}{}
				if rGW, ok := respSnmpGW["result"]; ok {
					if arr, ok2 := rGW.([]interface{}); ok2 {
						for _, raw := range arr {
							if item, ok3 := raw.(map[string]interface{}); ok3 {
								if hosts, ok4 := item["hosts"].([]interface{}); ok4 {
									for _, h := range hosts {
										if hm, ok5 := h.(map[string]interface{}); ok5 {
											modernHostids[fmt.Sprintf("%v", hm["hostid"])] = struct{}{}
										}
									}
								}
							}
						}
					}
				}
				// Coleta os hostids de TODOS os templates SNMP que possuem pelo menos um item não moderno
				legacyHostSet := map[string]struct{}{}
				if rAll, ok := respSnmpAll["result"]; ok {
					if arr, ok2 := rAll.([]interface{}); ok2 {
						for _, raw := range arr {
							if item, ok3 := raw.(map[string]interface{}); ok3 {
								if hosts, ok4 := item["hosts"].([]interface{}); ok4 {
									for _, h := range hosts {
										if hm, ok5 := h.(map[string]interface{}); ok5 {
											hid := fmt.Sprintf("%v", hm["hostid"])
											if _, isModern := modernHostids[hid]; !isModern {
												legacyHostSet[hid] = struct{}{}
											}
										}
									}
								}
							}
						}
					}
				}
				if len(legacyHostSet) > 0 {
					legacyIds := []string{}
					for hid := range legacyHostSet { legacyIds = append(legacyIds, hid) }
					if tplResp, tplErr := zabbixApiRequest(apiUrl, token, "template.get", map[string]interface{}{
						"output":      []string{"templateid", "name"},
						"templateids": legacyIds,
						"selectHosts": []string{"hostid"}, // apenas templates vinculados a pelo menos um host
					}); tplErr == nil {
						if rTpl, ok := tplResp["result"]; ok {
							if arr, ok2 := rTpl.([]interface{}); ok2 {
								for _, raw := range arr {
									if tm, ok3 := raw.(map[string]interface{}); ok3 {
										// Descarta temtlates não utilizados por nenhum host
										hosts, _ := tm["hosts"].([]interface{})
										if len(hosts) == 0 { continue }
										snmpMigrationTpls = append(snmpMigrationTpls, fmt.Sprintf("%v", tm["name"]))
									}
								}
							}
						}
					}
					sort.Strings(snmpMigrationTpls)
				}
			}
		}
	}

	// --- Recommendations KPI row + lightweight cards (modern layout, anchors scroll to sections) ---
	// KPI numbers (computed after attention and interval aggregates are available)
	attentionCount := len(attention)
	proxyOfflineCount := offline
	proxyUnknownCount := unknown
	unsupportedCount := unsupportedVal
	textItemsCount := textCount

	// number of templates shown in "Templates para revisão" (cap to topN)
	templatesShown := len(topTemplates)
	if templatesShown > topN { templatesShown = topN }

	html += `<style>
.rec-kpis{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:14px}
.kpi{min-width:150px;padding:12px;border-radius:8px;background:#fff;box-shadow:0 2px 8px rgba(0,0,0,0.04);cursor:pointer;display:flex;flex-direction:column;align-items:flex-start}
.kpi .kpi-num{font-weight:700;font-size:20px}
.kpi .kpi-label{font-size:12px;color:#334155}
.kpi-warn{border-left:4px solid #ffcc00}
.kpi-crit{border-left:4px solid #ff6666}
.kpi-ok{border-left:4px solid #16a34a}
.rec-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:12px;margin-bottom:14px}
.rec-card{background:#fff;padding:10px;border-radius:8px;box-shadow:0 2px 10px rgba(0,0,0,0.04)}
.rec-card-header{display:flex;align-items:center;justify-content:space-between;gap:8px}
.status-badge{padding:4px 8px;border-radius:999px;font-weight:600;font-size:12px}
.status-badge.ok{background:#e6ffef;color:#065f46}
.status-badge.warn{background:#fff7e6;color:#b26b00}
.status-badge.crit{background:#fff1f0;color:#b02a2a}
.rec-toggle{background:#eef2ff;border:0;padding:6px 8px;border-radius:6px;cursor:pointer}
</style>`
	html += `<div class='rec-grid'>` +
		`<div id='card-server' class='rec-card'><div class='rec-card-header'><strong>Zabbix Server</strong><span class='status-badge ` + func() string { if attentionCount>0 { return "warn" } ; return "ok" }() + `'>` + fmt.Sprintf("%d Processos e Threads", attentionCount) + `</span></div><div class='rec-card-body'><!-- server content below --></div></div>` +
		`<div id='card-proxys' class='rec-card'><div class='rec-card-header'><strong>Zabbix Proxys</strong><span class='status-badge ` + func() string { if proxyOfflineCount>0 || proxyUnknownCount>0 { return "crit" } ; return "ok" }() + `'>` + fmt.Sprintf("%d/%d", proxyOfflineCount, proxyUnknownCount) + `</span></div><div class='rec-card-body'><!-- proxy content below --></div></div>` +
		`<div id='card-items' class='rec-card'><div class='rec-card-header'><strong>Items</strong><span class='status-badge ` + func() string { if unsupportedCount>0 { return "crit" } ; return "ok" }() + `'>` + fmt.Sprintf("%d não suportados", unsupportedCount) + `</span></div><div class='rec-card-body'><!-- items content below --></div></div>` +
		`<div id='card-lld' class='rec-card'><div class='rec-card-header'><strong>Regras de LLD</strong><span class='status-badge ok'>` + fmt.Sprintf("%d curto intervalo", lldLe300) + `</span></div><div class='rec-card-body'><!-- lld content below --></div></div>` +
		`<div id='card-templates' class='rec-card'><div class='rec-card-header'><strong>Templates</strong><span class='status-badge ok'>` + fmt.Sprintf("%d", templatesShown) + `</span></div><div class='rec-card-body'><!-- templates content below --></div></div>` +
	`</div>`
	// SNMP-POLLER KPI (porcentagem)
	snmpPct := 0.0
	if snmpTplCount > 0 { snmpPct = (float64(snmpGetWalkCount) * 100.0) / float64(snmpTplCount) }
	html += `<div class='rec-kpis'>`
	html += `<div class='kpi kpi-warn' data-target='#card-server' title='Processos em Atenção'><div class='kpi-num'>` + fmt.Sprintf("%d", attentionCount) + `</div><div class='kpi-label'>Zabbix Server - Process/Pollers com AVG alto</div></div>`
	html += `<div class='kpi kpi-crit' data-target='#card-proxys' title='Proxys offline'><div class='kpi-num'>` + fmt.Sprintf("%d", proxyOfflineCount) + `</div><div class='kpi-label'>Proxys Offline</div></div>`
	html += `<div class='kpi' data-target='#card-proxys' title='Proxys unknown'><div class='kpi-num'>` + fmt.Sprintf("%d", proxyUnknownCount) + `</div><div class='kpi-label'>Proxys Unknown</div></div>`
	// KPI: processos dos proxys com AVG alto (≥ 60%)
	proxyAttnClass := "kpi-ok"
	if len(proxyProcAttnList) > 0 { proxyAttnClass = "kpi-warn" }
	html += `<div class='kpi ` + proxyAttnClass + `' data-target='#card-proxys' title='Proxys - Processos em Atenção'><div class='kpi-num'>` + fmt.Sprintf("%d", len(proxyProcAttnList)) + `</div><div class='kpi-label'>Proxys - Process/Pollers com AVG alto</div></div>`
	html += `<div class='kpi kpi-crit' data-target='#card-items' title='Items não suportados'><div class='kpi-num'>` + fmt.Sprintf("%d", unsupportedCount) + `</div><div class='kpi-label'>Items Não Suportados</div></div>`
	// show SNMP KPIs only for Zabbix 7 (we computed counts earlier)
	if majorV >= 7 {
		// KPI: Templates SNMP que ainda precisam migrar para o poller assíncrono (get[]/walk[])
		migClass := "kpi-ok"
		if len(snmpMigrationTpls) > 0 { migClass = "kpi-warn" }
		html += `<div class='kpi ` + migClass + `' data-target='#card-templates' title='Templates SNMP para Poller Assíncrono'><div class='kpi-num'>` + fmt.Sprintf("%d", len(snmpMigrationTpls)) + `</div><div class='kpi-label'>Templates SNMP p/ Poller Assíncrono</div></div>`
		// KPI: Percentual de items SNMP em templates já usando get[]/walk[]
		kclass := "kpi-crit"
		if snmpPct >= 80.0 { kclass = "kpi-ok" }
		html += `<div class='kpi ` + kclass + `' data-target='#card-items' title='Items - SNMP-POLLER'><div class='kpi-num'>` + fmt.Sprintf("%.2f%%", snmpPct) + `</div><div class='kpi-label'>Items - SNMP-POLLER</div></div>`
	}
	html += `<div class='kpi kpi-warn' data-target='#card-items' title='Items Texto com Histórico'><div class='kpi-num'>` + fmt.Sprintf("%d", textItemsCount) + `</div><div class='kpi-label'>Items Texto c/ Histórico</div></div>`
	html += `</div>`	

	html += `<script>
document.querySelectorAll('.rec-kpis .kpi').forEach(k=>k.addEventListener('click',function(){var t=this.getAttribute('data-target');if(!t)return;var el=document.querySelector(t);if(el)el.scrollIntoView({behavior:'smooth',block:'start'})}));
// ensure our info tooltips still work after possible dynamic toggles
setTimeout(setupInfoTooltips,50);
</script>`

	// Recomendações dinâmicas (uses precomputed aggregates above)
	// secNum auto-increments each time a top-level section is emitted;
	// subNum resets to 1 at each new section and increments per sub-item.
	secNum := 0
	nextSec := func(cardID, title string) string {
		secNum++
		return fmt.Sprintf("<h4 id='%s'>%d) %s</h4>", cardID, secNum, title)
	}
	nextSub := func(sub *int, label string) string {
		*sub++
		return fmt.Sprintf("%d.%d)", secNum, *sub) + " " + label
	}

	// --- Seção: Zabbix Server (sempre mostrada) ---
	serverSub := 0
	html += nextSec("card-server", "Zabbix Server")
	html += fmt.Sprintf("<h5>%d.%d) Sugestões zabbix_server.conf:</h5>", secNum, 1)
	serverSub = 1
	checkTrendEnvVal := os.Getenv("CHECKTRENDTIME")
	if checkTrendEnvVal == "" { checkTrendEnvVal = "15d" }
	tipProc := fmt.Sprintf("Aumente os Processos e Threads conforme a necessidade da empresa; atualmente a leitura é realizada com base em %s (%s) e validando em Trends. Se o valor de AVG for maior que 60%%, é sugerido aumentar.", checkTrendEnvVal, checkTrendDisplay)
	html += titleWithInfo("h5", nextSub(&serverSub, "Customizar Processos e Threads"), tipProc)
	if len(attention) == 0 {
		html += `<p>Nenhum processo em estado de Atenção detectado.</p>`
	} else {
		html += `<ol style='margin-left:18px;'>`
		for _, a := range attention {
			html += `<li>` + htmlpkg.EscapeString(a.Name) + ` — média: ` + fmt.Sprintf("%.2f%%", a.Vavg) + `</li>`
		}
		html += `</ol>`
	}

	// Sugestão: Pollers Assíncronos (mostrar somente se algum não estiver habilitado)
	asyncNames := []string{"Agent Poller", "HTTP Agent Poller", "SNMP Poller"}
	missingAsync := []string{}
	// build a normalized map of poller friendly name (lowercase) -> pollRow for lookup
	pollMap := map[string]pollRow{}
	for _, pr := range pollRows {
		key := strings.ToLower(strings.TrimSpace(pr.Friendly))
		pollMap[key] = pr
	}
	for _, an := range asyncNames {
		norm := strings.ToLower(strings.TrimSpace(an))
		if pr, ok := pollMap[norm]; ok {
			if pr.Disabled && strings.TrimSpace(pr.DisabledMsg) != "" {
				missingAsync = append(missingAsync, an)
			}
		}
	}
	if len(missingAsync) > 0 {
		tipAsync := "Se utilizado os items para serem monitorados pelo Zabbix Server, configure 1 processo poller para ser utilizado até 1000 checks em conjunto por poller, evitando esperas síncronas. Pode ser ajustado o número de processos nos arquivos de configuração (ex.: zabbix_server.conf) conforme a carga do ambiente. Novidade do Zabbix 7."
		html += titleWithInfo("h5", nextSub(&serverSub, "Utilizar Pollers Assíncronos:"), tipAsync)
		html += `<div style='margin-left:6px;'><ul>`
		descs := map[string]string{
			"Agent Poller": "Para checks passivos utilizando items do tipo `Zabbix Agent`.",
			"HTTP Agent Poller": "Para verificações utilizando items do Tipo `HTTP Agent`.",
			"SNMP Poller": "Para verificações SNMP utilizando snmp_oid get[] e walk[].",
		}
		for _, n := range missingAsync {
			d := descs[n]
			html += `<li>` + titleWithInfo("span", n, d) + `</li>`
		}
		html += `</ul></div>`
	}

	// --- Seção: Zabbix Proxys (Unknown, Offline ou processos em Atenção ou sem template) ---
	if unknown > 0 || offline > 0 || len(proxyProcAttnList) > 0 || len(proxyNoTemplateList) > 0 {
		proxySub := 0
		html += nextSec("card-proxys", "Zabbix Proxys")
		if len(proxyProcAttnList) > 0 {
			tipProxyProc := fmt.Sprintf("Aumente os Processos e Threads conforme a necessidade da empresa; atualmente a leitura é realizada com base em %s (%s) e validando em Trends. Se o valor de AVG for maior que 60%%, é sugerido aumentar.", checkTrendStr, checkTrendDisplay)
			html += titleWithInfo("h5", nextSub(&proxySub, "Customizar Processos e Threads"), tipProxyProc)
			html += `<ol style='margin-left:18px;'>`
			for _, a := range proxyProcAttnList {
				html += `<li>` + htmlpkg.EscapeString(a.ProxyName) + ` — ` + htmlpkg.EscapeString(a.ProcFriendly) + ` — média: ` + fmt.Sprintf("%.2f%%", a.Vavg) + `</li>`
			}
			html += `</ol>`
		}
		if unknown > 0 {
			tipUnknown := "Verifique se o proxy está acessível na rede e se o serviço está ativo. " +
				"Cheque " + ambienteUrl + " -> Proxies para detalhes e tente reiniciar o proxy se necessário. " +
				"Confirme versões e compatibilidade (campo version no registro do proxy)."
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&proxySub, "Status Proxys Unknown"))
			html += `<p>Foram detectados ` + fmt.Sprintf("%d", unknown) + ` proxys com status ` + titleWithInfo("span", "Unknown", tipUnknown) + `</p>`
			html += `<ul>`
			for _, n := range unknownNames { html += `<li>` + htmlpkg.EscapeString(n) + `</li>` }
			html += `</ul>`
		}
		if offline > 0 {
			tipOffline := "Verifique se o proxy está acessível na rede e se o serviço está ativo. " +
				"Cheque " + ambienteUrl + " -> Proxies para detalhes e tente reiniciar o proxy se necessário. " +
				"Confirme versões e compatibilidade (campo version no registro do proxy)."
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&proxySub, "Proxys Offline"))
			html += `<p>Foram detectados ` + fmt.Sprintf("%d", offline) + ` proxys com status ` + titleWithInfo("span", "Offline", tipOffline) + `</p>`
			html += `<ul>`
			for _, n := range offlineNames { html += `<li>` + htmlpkg.EscapeString(n) + `</li>` }
			html += `</ul>`
		}
		if len(proxyNoTemplateList) > 0 {
			// build link to template search in the Zabbix frontend
			tplSearchPath := "/zabbix.php?action=template.list&filter_set=1&filter_name=Zabbix+Proxy+Health"
			if majorV < 7 {
				tplSearchPath = "/templates.php?filter_set=1&filter_name=Zabbix+Proxy+Health"
			}
			tplSearchLink := `<a href='` + htmlpkg.EscapeString(ambienteUrl+tplSearchPath) + `' target='_blank' rel='noopener'>Zabbix Proxy Health</a>`
			tipNoTpl := "O template \"Zabbix Proxy Health\" (ou equivalente \"Template App Zabbix Proxy\" no Zabbix 6) fornece os itens internos " +
				"(type=5) de monitoramento de processos do proxy. Sem ele vinculado ao host do proxy, " +
				"não é possível coletar métricas de pollers e processos."
			html += titleWithInfo("h5", nextSub(&proxySub, "Zabbix Proxy sem Monitoramento"), tipNoTpl)
			html += `<p>Os proxies abaixo não possuem o template ` + tplSearchLink + ` vinculado ao seu host, ou o host não existe. ` +
				`Sem este template, os dados de processo/poller não são coletados e fica sem visibilidade para o proxy.</p>`
			html += `<ol style='margin-left:18px;'>`
			for _, pt := range proxyNoTemplateList {
				// Zabbix 7: filter_name (campo display name); Zabbix 6: filter_host (campo technical name)
				// Ambos recebem &filter_set=1 para aplicar o filtro automaticamente
				hostLinkPath := "/zabbix.php?action=host.list&filter_name=" + htmlpkg.EscapeString(pt.ProxyName) + "&filter_set=1"
				if majorV < 7 {
					hostLinkPath = "/zabbix.php?action=host.list&filter_host=" + htmlpkg.EscapeString(pt.ProxyName) + "&filter_set=1"
				}
				hostLink := `<a href='` + htmlpkg.EscapeString(ambienteUrl+hostLinkPath) + `' target='_blank' rel='noopener'>` + htmlpkg.EscapeString(pt.ProxyName) + `</a>`
				html += `<li>` + hostLink + `</li>`
			}
			html += `</ol>`
		}
		html += `<div style='height:8px'></div>`
	}

	// --- Seção: Items (sempre mostrada) ---
	disabledCount := 0
	if nItemsDisabled != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItemsDisabled)); err == nil { disabledCount = v }
	}
	pct := func(part, total int) string {
		if total <= 0 { return "0%" }
		return fmt.Sprintf("%.2f%%", (float64(part)*100.0)/float64(total))
	}
	itemsSub := 0
	html += nextSec("card-items", "Items")
	html += `<div style='margin-left:6px;'>`
	html += `<p><strong>` + nextSub(&itemsSub, "Items sem Template:") + `</strong> Existem ` + fmt.Sprintf("%d", itemsNoTplCount) + ` items sem template. Validar a necessidade de criação de template para estes items; não impacta diretamente na performance do Zabbix, porém é útil para organização e reutilização dos items. Lista de itens na aba Items e LLD na opção "Items sem Template"</p>`
	html += `<p><strong>` + nextSub(&itemsSub, "Items não suportados:") + `</strong> Existem ` + fmt.Sprintf("%d", unsupportedVal) + ` items não suportados, cerca de ` + pct(unsupportedVal, totalItemsVal) + ` do total. São items ativos que apresentaram erro na coleta e continuam consumindo processos do Zabbix desnecessariamente. Lista de itens na aba Items e LLD na opção "Items não suportados"</p>`
	html += `<p><strong>` + nextSub(&itemsSub, "Items desabilitados:") + `</strong> Existem ` + fmt.Sprintf("%d", disabledCount) + ` items desabilitados, cerca de ` + pct(disabledCount, totalItemsVal) + ` do total. Não consomem processos, mas é necessário avaliar o motivo e o impacto no monitoramento. Lista de itens na aba Items e LLD na opção "Items desabilitados"</p>`
	html += `<p><strong>` + nextSub(&itemsSub, "Items com Intervalo ≤ 60s:") + `</strong> Existem ` + fmt.Sprintf("%d", itemsLe60) + ` items com intervalo de coleta ≤ 60s. Quanto menor o intervalo, maior o consumo de CPU, memória e crescimento do banco de dados. Avalie a real necessidade. Lista de itens na aba Items e LLD na opção "Intervalo de Coleta"</p>`
	if textCount > 0 {
		html += `<p><strong>` + nextSub(&itemsSub, "Items Texto com Histórico (≤ 300s):") + `</strong> Existem ` + fmt.Sprintf("%d", textCount) + ` items do tipo Texto com retenção de histórico e intervalo ≤ 300s. Items de Texto têm custo elevado em disco; prefira não reter histórico (Do not store) ou use preprocessamento/item dependente. Lista de itens na aba Items e LLD na opção "Items Texto com Historico"</p>`
	}
	if majorV >= 7 && snmpTplCount > 0 {
		tipSnmp := htmlpkg.EscapeString("Esses SNMP OID utilizam o Poller Assíncrono 'SNMP Poller' do Zabbix 7, que tende a ter melhor performance para ambientes com muitos checks SNMP. Considere migrar templates/items para este formato.")
		snmpIcon := `<span class='info-icon' tabindex='0' style='display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;cursor:pointer;margin-left:4px;position:relative;vertical-align:middle;'>` +
			`<svg viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='1.6' fill='white'/><text x='8' y='11' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
			`<span class='info-tooltip'>` + tipSnmp + `</span></span>`
		html += `<p><strong>` + nextSub(&itemsSub, "Items SNMP-POLLER (Zabbix 7):") + `</strong>` + snmpIcon + ` Existem ` + fmt.Sprintf("%d", snmpTplCount) + ` items SNMP em Templates, porém somente ` + fmt.Sprintf("%d", snmpGetWalkCount) + ` utilizando SNMP OID com get[] e walk[], cerca de ` + pct(snmpGetWalkCount, totalItemsVal) + ` do total. Vericar os templates em "Templates passíveis para migração para SNMP-POLLER" na sessão Templates. </p>`
	}
	html += `</div>`

	// --- Seção: Regras de LLD ---
	lldSub := 0
	html += nextSec("card-lld", "Regras de LLD")
	html += `<div style='margin-left:6px;'>`
	html += `<p><strong>` + nextSub(&lldSub, "Regras de LLD com Intervalo ≤ 300s:") + `</strong> Existem ` + fmt.Sprintf("%d", lldLe300) + ` regras de LLD com intervalo de coleta ≤ 300s. LLD cria itens/triggers/gráficos automaticamente; na maioria dos casos não há necessidade de descoberta a cada minuto, o que impacta diretamente o processo interno LLD Manager.</p>`
	html += `<p><strong>` + nextSub(&lldSub, "Regras de LLD não suportadas:") + `</strong> Existem ` + fmt.Sprintf("%d", lldNotSupCnt) + ` regras de LLD com status Não Suportado. Necessita validação e correção; impacta diretamente o processo interno LLD Manager.</p>`
	html += `</div>`

	// --- Seção: Templates ---
	tplSub := 0
	html += "<div id='card-templates'></div>"
	html += titleWithInfo("h4", fmt.Sprintf("%d) Templates", secNum+1), descTemplates+" Para revisão dos templates e itens problemáticos, utilize as informações contidas na guia Templates.")
	secNum++ // avança manualmente pois o título já foi emitido via titleWithInfo
	html += `<div style='margin-left:6px;'>`
	html += fmt.Sprintf("<h5>%d.%d) Templates para revisão - Detalhes na aba Templates</h5>", secNum, func() int { tplSub++; return tplSub }())
	if len(topTemplates) == 0 {
		html += `<p>Nenhum template problemático identificado.</p>`
	} else {
		html += `<ul>`
		cnt := 0
		for _, t := range topTemplates {
			if cnt >= 10 { break }
			name := templateNames[t.Key]
			if name == "" { name = t.Key }
			html += `<li>` + htmlpkg.EscapeString(name) + `</li>`
			cnt++
		}
		html += `</ul>`
	}
	html += fmt.Sprintf("<h5>%d.%d) Erros Mais Comuns - Detalhes na aba Templates</h5>", secNum, func() int { tplSub++; return tplSub }())
	if len(topErrors) == 0 {
		html += `<p>Nenhum erro identificado.</p>`
	} else {
		html += `<ul>`
		cnt2 := 0
		for _, e := range topErrors {
			if cnt2 >= 10 { break }
			parts := strings.SplitN(e.Key, "|", 2)
			errMsg := parts[0]
			tplId := ""
			if len(parts) > 1 { tplId = parts[1] }
			tplName := templateNames[tplId]
			if tplName == "" { tplName = tplId }
			html += `<li>` + htmlpkg.EscapeString(errMsg) + ` - ` + htmlpkg.EscapeString(tplName) + `</li>`
			cnt2++
		}
		html += `</ul>`
	}
	if majorV >= 7 && len(snmpMigrationTpls) > 0 {
		tipSnmpMig := "Templates que possuem items SNMP ainda utilizando OID no formato antigo (sem get[] ou walk[]). Esses items usam o poller síncrono padrão. Migrar para o formato get[]/walk[] permite que o Zabbix 7 utilize o 'SNMP Poller' assíncrono, melhorando a performance de coleta em ambientes com muitos checks SNMP."
		html += titleWithInfo("h5", fmt.Sprintf("%d.%d) Templates passíveis para migração para SNMP-POLLER", secNum, func() int { tplSub++; return tplSub }()), tipSnmpMig)
		html += `<ul>`
		for _, name := range snmpMigrationTpls {
			html += `<li>` + htmlpkg.EscapeString(name) + `</li>`
		}
		html += `</ul>`
	}
	html += `</div>`
	html += `</div>`

	// small JS to handle tab switching (keeps markup simple and UX clean)
	html += `<script>` +
		`function showTab(id){` +
			`var panels=document.querySelectorAll('.tab-panel');` +
			`panels.forEach(function(p){p.style.display='none';});` +
			`var el=document.getElementById(id); if(el) el.style.display='block';` +
			`var btns=document.querySelectorAll('.tab-btn');` +
			`btns.forEach(function(b){b.classList.remove('active');});` +
			`var active=document.querySelector(".tab-btn[data-tab='"+id+"']"); if(active){ active.classList.add('active'); }` +
		`}` +
		`document.querySelectorAll('.tab-btn').forEach(function(b){ b.addEventListener('click', function(){ showTab(this.getAttribute('data-tab')); }); });` +
	`</script>`
	html += `</div>`
	return html, nil
}

func main() {
	// initialize debug flag from ENV
	v := strings.ToLower(strings.TrimSpace(os.Getenv("APP_DEBUG")))
	if v == "1" || v == "true" || v == "yes" {
		debugApi = true
		log.Printf("[DEBUG] APP_DEBUG enabled: verbose API logs ON")
	}
	// initialize trend window from ENV CHECKTRENDTIME
	parseCheckTrendEnv()
	r := gin.Default()

	r.Static("/static", "./web/static")
	r.LoadHTMLGlob("web/templates/*")

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})

	// initialize Postgres (optional) using ENV vars
	var db *sql.DB
	dbHost := os.Getenv("DB_HOST")
	if dbHost != "" {
		dbPort := os.Getenv("DB_PORT")
		if dbPort == "" { dbPort = "5432" }
		dbUser := os.Getenv("DB_USER")
		if dbUser == "" { dbUser = "postgres" }
		dbPass := os.Getenv("DB_PASSWORD")
		if dbPass == "" { dbPass = "postgres" }
		dbName := os.Getenv("DB_NAME")
		if dbName == "" { dbName = "zabbix_report" }
		conn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", dbHost, dbPort, dbUser, dbPass, dbName)

		// Attempt to connect with retries. If still unavailable after attempts, exit so container stops.
		maxAttempts := 15
		waitSec := 2
		var d *sql.DB
		var err error
		for i := 1; i <= maxAttempts; i++ {
			d, err = sql.Open("postgres", conn)
			if err == nil {
				if pingErr := d.Ping(); pingErr == nil {
					db = d
					break
				} else {
					_ = d.Close()
					err = pingErr
				}
			}
			log.Printf("[WARN] Postgres not available (attempt %d/%d): %v", i, maxAttempts, err)
			time.Sleep(time.Duration(waitSec) * time.Second)
		}
		if db == nil {
			log.Fatalf("[FATAL] Postgres did not become available after %d attempts: %v", maxAttempts, err)
		}

		log.Printf("[INFO] Connected to Postgres at %s:%s db=%s", dbHost, dbPort, dbName)
		// ensure reports table has the correct schema; if an old table exists with missing columns,
		// rename it to preserve data and create a fresh table with the expected columns
		expectedCols := map[string]bool{"id":false, "name":false, "format":false, "content":false, "zabbix_url":false, "created_at":false}
		rows, err := db.Query("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='reports'")
		if err != nil {
			log.Printf("[WARN] Failed to inspect reports table schema: %v", err)
		} else {
			defer rows.Close()
			found := 0
			for rows.Next() {
				var col string
				_ = rows.Scan(&col)
				if _, ok := expectedCols[col]; ok {
					expectedCols[col] = true
				}
				found++
			}
			// if table doesn't exist, create it
			if found == 0 {
				_, err := db.Exec(`CREATE TABLE reports (
					id SERIAL PRIMARY KEY,
					name TEXT,
					format TEXT,
					content BYTEA,
					zabbix_url TEXT,
					created_at TIMESTAMPTZ DEFAULT now()
				)`)
				if err != nil {
					log.Printf("[WARN] Failed to create reports table: %v", err)
				}
			} else {
				// if any expected column is missing, rename old table and create a fresh one
				missing := []string{}
				for k, v := range expectedCols {
					if !v { missing = append(missing, k) }
				}
				if len(missing) > 0 {
					ts := time.Now().Unix()
					oldName := fmt.Sprintf("reports_old_%d", ts)
					_, err := db.Exec(fmt.Sprintf("ALTER TABLE reports RENAME TO %s", oldName))
					if err != nil {
						log.Printf("[WARN] Failed to rename old reports table: %v", err)
					} else {
						log.Printf("[INFO] Renamed existing reports -> %s due to missing columns: %v", oldName, missing)
						_, err := db.Exec(`CREATE TABLE reports (
							id SERIAL PRIMARY KEY,
							name TEXT,
							format TEXT,
							content BYTEA,
							zabbix_url TEXT,
							created_at TIMESTAMPTZ DEFAULT now()
						)`)
						if err != nil {
							log.Printf("[WARN] Failed to create new reports table: %v", err)
						}
					}
				}
			}
		}
	}

	// In-memory task store (keeps recent tasks until process restart)
	type Task struct {
		ID          string
		Status      string // "pending", "processing", "done", "error"
		Report      string
		ProgressMsg string // mensagem de progresso
		DBID        int
	}
	var tasks = make(map[string]*Task)
	var tasksMu sync.RWMutex
	getTask := func(id string) *Task {
		tasksMu.RLock(); defer tasksMu.RUnlock()
		return tasks[id]
	}
	setTask := func(id string, t *Task) {
		tasksMu.Lock(); defer tasksMu.Unlock()
		tasks[id] = t
	}

	r.POST("/api/start", func(c *gin.Context) {
		type Req struct {
			ZabbixURL   string `json:"zabbix_url"`
			ZabbixToken string `json:"zabbix_token"`
		}
		var req Req
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("[ERROR] Dados inválidos recebidos: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos"})
			return
		}
		if debugApi {
			log.Printf("[DEBUG] Requisição recebida: url=%s, token=<redacted>", req.ZabbixURL)
		}
		id := fmt.Sprintf("task-%d", time.Now().UnixNano())
		setTask(id, &Task{ID: id, Status: "processing", ProgressMsg: "Iniciando coleta..."})
		go func(taskID string, url, token string) {
			setProgress := func(msg string) {
				if t := getTask(taskID); t != nil { t.ProgressMsg = msg }
			}
			setProgress("Detectando versão do Zabbix...")
			report, err := generateZabbixReportWithProgress(url, token, setProgress)
			if err != nil {
				log.Printf("[ERROR] Erro na tarefa %s: %v", taskID, err)
				if t := getTask(taskID); t != nil {
					tasksMu.Lock()
					t.Status = "error"
					if strings.Contains(err.Error(), "Not authorized") || strings.Contains(err.Error(), "Not authorised") {
						t.Report = "<div style='color:red;'>Token Invalido</div>"
					} else {
						t.Report = "<div style='color:red;'>Erro: " + err.Error() + "</div>"
					}
					tasksMu.Unlock()
				}
				return
			}
			log.Printf("[DEBUG] Tarefa %s concluída", taskID)
			if t := getTask(taskID); t != nil {
				tasksMu.Lock()
				t.Status = "done"
				t.Report = report
				t.ProgressMsg = "Relatório gerado." // final
				tasksMu.Unlock()
			}
			// save to DB if available
			if db != nil {
				name := "report-" + fmt.Sprintf("%d", time.Now().Unix())
				// insert
				_, err := db.Exec("INSERT INTO reports(name, format, content, zabbix_url) VALUES($1,$2,$3,$4)", name, "html", []byte(report), url)
				if err != nil {
					log.Printf("[WARN] Failed to save report to DB: %v", err)
				}
			}
			return
		}(id, req.ZabbixURL, req.ZabbixToken)
		c.JSON(http.StatusOK, gin.H{"task_id": id})
	})

				// progress endpoint for in-memory tasks
				r.GET("/api/progress/:id", func(c *gin.Context) {
					id := c.Param("id")
					task := getTask(id)
					if task == nil {
						c.JSON(http.StatusNotFound, gin.H{"error": "Tarefa não encontrada"})
						return
					}
					// Include the optional report HTML so callers can show a meaningful error message
					c.JSON(http.StatusOK, gin.H{"status": task.Status, "progress_msg": task.ProgressMsg, "report": task.Report})
				})

				// return report HTML generated in this session (in-memory)
				r.GET("/api/report/:id", func(c *gin.Context) {
					id := c.Param("id")
					task := getTask(id)
					if task == nil || task.Status != "done" {
						c.JSON(http.StatusNotFound, gin.H{"error": "Relatório não disponível"})
						return
					}
					c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(task.Report))
				})

				// db-status: informa ao frontend se o banco está configurado
				r.GET("/api/db-status", func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"db_enabled": db != nil})
				})

				// list reports stored in DB (most recent first)
				r.GET("/api/reports", func(c *gin.Context) {
					if db == nil {
						c.JSON(http.StatusOK, gin.H{"db_enabled": false, "reports": []interface{}{}})
						return
					}
					limit := 20
					rows, err := db.Query("SELECT id, name, format, zabbix_url, created_at FROM reports ORDER BY created_at DESC LIMIT $1", limit)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					defer rows.Close()
					out := []map[string]interface{}{}
					for rows.Next() {
						var id int
						var name, format, zurl string
						var created time.Time
						if err := rows.Scan(&id, &name, &format, &zurl, &created); err == nil {
							out = append(out, map[string]interface{}{"id": id, "name": name, "format": format, "zabbix_url": zurl, "created_at": created})
						}
					}
					c.JSON(http.StatusOK, gin.H{"db_enabled": true, "reports": out})
				})

// fetch report content from DB by id
			// ?raw=1  → return only the HTML fragment so the JS renderReport() can
			//            assemble the full layout (header + export/print buttons) itself.
			// (default) → wrap fragment in a full standalone document (legacy / direct link).
			r.GET("/api/reportdb/:id", func(c *gin.Context) {
				if db == nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "DB not configured"})
					return
				}
				id := c.Param("id")
				row := db.QueryRow("SELECT name, format, content FROM reports WHERE id = $1", id)
				var name, format string
				var content []byte
				if err := row.Scan(&name, &format, &content); err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "Relatório não encontrado"})
					return
				}
				s := string(content)
				low := strings.ToLower(s)
				isFullDoc := strings.Contains(low, "<!doctype") || strings.Contains(low, "<html")

				// ?raw=1: JS wants a bare fragment to render inline with its own layout
				if c.Query("raw") == "1" {
					fragment := s
					if isFullDoc {
						// Buscar <body e </body> diretamente em s (case-insensitive via lowS)
						// para garantir que os índices correspondem ao string original s.
						lowS := strings.ToLower(s)
						bi := strings.Index(lowS, "<body")
						ei := strings.LastIndex(lowS, "</body>")
						if bi != -1 && ei != -1 {
							// advance past the closing ">" of the <body ...> tag
							bodyTagEnd := strings.Index(s[bi:], ">")
							if bodyTagEnd != -1 {
								fragment = s[bi+bodyTagEnd+1 : ei]
							}
						}
					}
					c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fragment))
					return
				}

				// default: return a standalone full document (no raw flag)
				if isFullDoc {
					c.Data(http.StatusOK, "text/html; charset=utf-8", content)
					return
				}
				// wrap fragment into a full document with CSS / JS / gauge init
				cssLink := `<link rel="stylesheet" href="/static/style.css">`
				jsChart := `<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>`
				extra := `<script src="/static/script.js"></script>`
					initInline := `<script>window.addEventListener('load', function(){ try{ if (typeof initGauges === 'function') initGauges(document.body); }catch(e){console&&console.error(e);} });</script>`
					full := "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Relatório Zabbix - " + htmlpkg.EscapeString(name) + "</title>" + cssLink + jsChart + `</head><body>` + s + extra + initInline + `</body></html>`
					c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(full))
				})

				// delete a single report by id
				r.DELETE("/api/reportdb/:id", func(c *gin.Context) {
					if db == nil {
						c.JSON(http.StatusNotFound, gin.H{"error": "DB not configured"})
						return
					}
					id := c.Param("id")
					res, err := db.Exec("DELETE FROM reports WHERE id = $1", id)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					n, _ := res.RowsAffected()
					if n == 0 {
						c.JSON(http.StatusNotFound, gin.H{"error": "Relatório não encontrado"})
						return
					}
					c.JSON(http.StatusOK, gin.H{"deleted": id})
				})

				// delete all reports
				r.DELETE("/api/reports", func(c *gin.Context) {
					if db == nil {
						c.JSON(http.StatusNotFound, gin.H{"error": "DB not configured"})
						return
					}
					res, err := db.Exec("DELETE FROM reports")
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					n, _ := res.RowsAffected()
					c.JSON(http.StatusOK, gin.H{"deleted": n})
				})

				r.Run(":8080")
			}

			// Wrapper for progress reporting
			func generateZabbixReportWithProgress(url, token string, setProgress func(string)) (string, error) {
				// install callback for use inside generateZabbixReport
				progressCb = setProgress
				// ensure we clear callback when finished
				defer func(){ progressCb = nil }()
				// initial message
				if progressCb != nil { progressCb("Detectando versão do Zabbix...") }
				// Call the original report generator which will call progressCb at key points
				return generateZabbixReport(url, token)
			}
