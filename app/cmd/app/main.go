package main

import (
	"github.com/gin-gonic/gin"
	collector "go-zabbix-report/internal/collector"
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
// useBearerAuth is set to true when the detected Zabbix version is >= 7.2.
// In that case all API calls (except user.login) must authenticate via
// "Authorization: Bearer <token>" HTTP header instead of the JSON-RPC "auth" field.
var useBearerAuth bool = false
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

// parseCountResult interpreta o campo result de uma resposta JSON-RPC do Zabbix
// para consultas com `countOutput:true`. Suporta `[]interface{}` (retorno antigo),
// números (float64) e strings que contenham números.
// NOTE: count parsing centralized in collector.CollectCount; old helper removed.

// Reusable HTTP client to improve performance (connection reuse)
var httpClient *http.Client
	var httpTransport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}

// Simple cache for item lookups: key is key+"|"+hostid -> map[string]interface{}
var itemLookupCache sync.Map

func initHttpClient() {
	if httpClient != nil {
		return
	}
	// Timeout padrão de 60s: trend.get e history.get em ambientes grandes podem levar 30-50s.
	// Configurável via ENV API_TIMEOUT_SECONDS para ajuste sem rebuild.
	timeoutSec := 60
	if v := os.Getenv("API_TIMEOUT_SECONDS"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 { timeoutSec = n }
	}
	log.Printf("[DEBUG] HTTP client timeout=%ds (API_TIMEOUT_SECONDS)", timeoutSec)
	httpTransport = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	}
	httpClient = &http.Client{Transport: httpTransport, Timeout: time.Duration(timeoutSec) * time.Second}
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

// isDeadlineError detecta erros de timeout do cliente HTTP.
// NÃO se deve retentar nesses casos: a API já está sob carga;
// retentar aumentaria a pressão e pioraria o problema.
func isDeadlineError(err error) bool {
	if err == nil { return false }
	s := err.Error()
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "Client.Timeout exceeded") ||
		strings.Contains(s, "i/o timeout")
}

// zabbixApiRequest é o ponto central de comunicação com a API JSON-RPC do Zabbix.
// Toda chamada à API (item.get, trend.get, host.get, etc.) passa por aqui.
//
// Parâmetros:
//
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
	// Zabbix < 7.2: token vai no campo "auth" do JSON-RPC.
	// Zabbix >= 7.2: token vai no header HTTP "Authorization: Bearer <token>".
	// user.login passa token vazio, então nenhum dos dois ramos se aplica.
	if token != "" && !useBearerAuth {
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
		if useBearerAuth && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if attempt > 1 {
			req.Header.Set("Connection", "close")
		}
		resp, err = httpClient.Do(req)
		if err == nil {
			break // sucesso
		}
		if isDeadlineError(err) {
			log.Printf("[ZABBIX API] %s timeout (tentativa %d/%d) — n\u00e3o retenta (API sobrecarregada): %v", method, attempt, maxRetries, err)
			break
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
	arr, err := collector.CollectRawList(apiUrl, token, "item.get", params, zabbixApiRequest)
	if err != nil { return nil, err }
	if len(arr) > 0 {
		m := arr[0]
		itemLookupCache.Store(cacheKey, m)
		return m, nil
	}
	return nil, nil
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
// process/proxy helpers migrated to app/internal/collector; wrappers removed.

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
	arr, err := collector.CollectRawList(apiUrl, token, "history.get", params, zabbixApiRequest)
	if err != nil { return "", err }
	if len(arr) > 0 {
		hist := arr[0]
		return fmt.Sprintf("%v", hist["value"]), nil
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
	arr, err := collector.CollectRawList(apiUrl, token, "history.get", params, zabbixApiRequest)
	if err != nil { return nil, err }
	if len(arr) == 0 { return nil, nil }
	var vals []float64
	for _, m := range arr {
		if m == nil { continue }
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
		"output":    []string{"itemid", "clock", "value_min", "value_avg", "value_max"},
		"itemids":   []string{itemid},
		"time_from": from,
		"time_to":   now,
	}
	arr, err := collector.CollectRawList(apiUrl, token, "trend.get", params, zabbixApiRequest)
	if err != nil { return nil, err }
	if len(arr) == 0 { return nil, nil }
	// Agrega todos os registros do período: min=menor, avg=média dos avgs, max=maior
	type aggState struct {
		vmin, vmax float64
		vavgSum    float64
		count      int
	}
	var agg *aggState
	parseF := func(row map[string]interface{}, k string) (float64, bool) {
		if v, ok2 := row[k]; ok2 {
			if f, e := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); e == nil { return f, true }
		}
		return 0, false
	}
	for _, row := range arr {
		if row == nil { continue }
		vmin, ok1 := parseF(row, "value_min")
		vavg, ok2 := parseF(row, "value_avg")
		vmax, ok3 := parseF(row, "value_max")
		if !ok1 && !ok2 && !ok3 { continue }
		if agg == nil {
			agg = &aggState{vmin: vmin, vmax: vmax, vavgSum: vavg, count: 1}
		} else {
			if ok1 && vmin < agg.vmin { agg.vmin = vmin }
			if ok3 && vmax > agg.vmax { agg.vmax = vmax }
			if ok2 { agg.vavgSum += vavg; agg.count++ }
		}
	}
	if agg == nil { return nil, nil }
	vavgFinal := 0.0
	if agg.count > 0 { vavgFinal = agg.vavgSum / float64(agg.count) }
	return map[string]interface{}{
		"value_min": fmt.Sprintf("%f", agg.vmin),
		"value_avg": fmt.Sprintf("%f", vavgFinal),
		"value_max": fmt.Sprintf("%f", agg.vmax),
	}, nil
	return nil, nil
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
	arr, err := collector.CollectRawList(apiUrl, token, "trend.get", params, zabbixApiRequest)
	if err != nil { return nil, err }
	type aggState struct {
		vmin, vmaxV float64
		vavgSum     float64
		count       int
	}
	agg := map[string]*aggState{}
	for _, row := range arr {
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
		arr, err := collector.CollectRawList(apiUrl, token, "history.get", params, zabbixApiRequest)
		if err != nil { continue }
		type aggS struct{ vals []float64 }
		agg := map[string]*aggS{}
		for _, row := range arr {
			if row == nil { continue }
			iid := fmt.Sprintf("%v", row["itemid"])
			if f, e := strconv.ParseFloat(fmt.Sprintf("%v", row["value"]), 64); e == nil {
				if agg[iid] == nil { agg[iid] = &aggS{} }
				agg[iid].vals = append(agg[iid].vals, f)
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
func generateZabbixReport(url, token string, progressCb func(string)) (string, error) {
		nItemsNaoSuportados := "-"
	if strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("zabbix URL is required")
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("zabbix API token is required")
	}

 	// Normalize inputs so validation and usage operate on the same values
 	url = strings.TrimSpace(url)
 	token = strings.TrimSpace(token)

	// restore apiUrl and html builder variables
	apiUrl := url
	html := ""
	log.Printf("[DEBUG] Iniciando coleta Zabbix: url=%s", url)
	// compute frontend base URL (ambienteUrl) early so links can be built
	ambienteUrl := url
	if strings.HasSuffix(ambienteUrl, "/api_jsonrpc.php") {
		ambienteUrl = ambienteUrl[:len(ambienteUrl)-len("/api_jsonrpc.php")]
	}
	// Normalize trailing slash before appending the API path
	if strings.HasSuffix(apiUrl, "/api_jsonrpc.php") {
		// already correct, keep as-is
	} else if strings.HasSuffix(apiUrl, "/") {
		apiUrl += "api_jsonrpc.php"
	} else {
		apiUrl += "/api_jsonrpc.php"
	}
	// Concurrency limit for parallel API calls (can be configured with env MAX_CCONCURRENT)
	// Default 4: evita sobrecarregar a API do Zabbix com muitas chamadas simultâneas.
	maxConcurrent := 4
	if v := os.Getenv("MAX_CCONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 { maxConcurrent = n }
	}
	log.Printf("[DEBUG] MAX_CCONCURRENT=%d", maxConcurrent)
	// semaphore channel used to bound concurrent API requests across sections
	sem := make(chan struct{}, maxConcurrent)

	// get Zabbix API version (apiinfo.version)
	zabbixVersion := ""
	if v, err := collector.CollectZabbixVersion(apiUrl, zabbixApiRequest); err == nil {
		zabbixVersion = v
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
	// A partir do Zabbix 7.2 a autenticação é via Bearer token no header HTTP.
	// Versões anteriores usam o campo "auth" no corpo JSON-RPC.
	useBearerAuth = majorV > 7 || (majorV == 7 && minorV >= 2)
	log.Printf("[DEBUG] Zabbix version=%s majorV=%d minorV=%d useBearerAuth=%v", zabbixVersion, majorV, minorV, useBearerAuth)

	// Helper: Funcao para formatar inteiros com ponto como separador de milhares (e.g. 16573 -> 16.573)
	formatInt := func(n int) string {
		neg := n < 0
		if neg { n = -n }
		s := strconv.Itoa(n)
		l := len(s)
		rem := l % 3
		var b strings.Builder
		i := 0
		if rem > 0 {
			b.WriteString(s[:rem])
			i = rem
		}
		for i < l {
			if b.Len() > 0 { b.WriteByte('.') }
			b.WriteString(s[i : i+3])
			i += 3
		}
		if neg { return "-" + b.String() }
		return b.String()
	}

	// Helper: recebe uma string possivelmente vinda da API e formata inteiros
	// com separador de milhares, preserva fragments HTML e não altera percentuais.
	formatMaybeNumber := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" || s == "-" { return "-" }
		if strings.Contains(s, "<") { return s }
		if strings.HasSuffix(s, "%") { return htmlpkg.EscapeString(s) }
		if v, err := strconv.Atoi(s); err == nil { return formatInt(v) }
		return htmlpkg.EscapeString(s)
	}

	// Helper: mapeia o nome de um processo Zabbix para o parâmetro de configuração
	// correspondente (StartODBCPollers, StartPollers, etc.).
	// Válido tanto para server quanto para proxy
	procToParam := func(procName string) string {
		lname := strings.ToLower(strings.TrimSpace(procName))
		switch {
		case strings.Contains(lname, "http") && strings.Contains(lname, "agent"):
			return "StartHTTPAgentPollers"
		case strings.Contains(lname, "http"):
			return "StartHTTPPollers"
		case strings.Contains(lname, "snmp") && strings.Contains(lname, "trapper"):
			return "StartSNMPTrapper"
		case strings.Contains(lname, "snmp"):
			return "StartSNMPPollers"
		case strings.Contains(lname, "unreachable"):
			return "StartPollersUnreachable"
		case strings.Contains(lname, "odbc"):
			return "StartODBCPollers"
		case strings.Contains(lname, "java"):
			return "StartJavaPollers"
		case strings.Contains(lname, "ipmi"):
			return "StartIPMIPollers"
		case strings.Contains(lname, "icmp"), strings.Contains(lname, "pinger"):
			return "StartPingers"
		case strings.Contains(lname, "browser"):
			return "StartBrowserPollers"
		case strings.Contains(lname, "agent"):
			return "StartAgentPollers"
		case strings.Contains(lname, "vmware"):
			return "StartVMwareCollectors"
		case strings.Contains(lname, "trapper"):
			return "StartTrappers"
		case strings.Contains(lname, "poller"):
			return "StartPollers"
		case strings.Contains(lname, "housekeep") || strings.Contains(lname, "housekeeper"):
			// Housekeeper internal process: suggest housekeeping-related params
			return "HousekeepingFrequency,MaxHousekeeperDelete"
		default:
			return ""
		}
	}

	// get em Consulta quantidade de itens não suportados
	if cnt, err := collector.CollectCount(apiUrl, token, "item.get", map[string]interface{}{"filter": map[string]interface{}{"state": 1, "status": 0}, "monitored": true}, zabbixApiRequest); err == nil {
		nItemsNaoSuportados = fmt.Sprintf("%d", cnt)
	}
	// ── Parallel summary collection ──────────────────────────────────────────────
	if progressCb != nil { progressCb("progress.collecting_users") }
	var (
		nUsers          int
		nUsersErr       error
		nTotalHosts     int
		nEnabledHosts   int
		nDisabledHosts  int
		hostsErr        error
		templatesCount  = "N/A"
		nItemsTotal     = "-"
		nItemsEnabled   = "-"
		nItemsDisabled  = "-"
	)
	var summaryWg sync.WaitGroup
	summaryWg.Add(5) // users, hosts(3-in-1), templates, items-total, items-enabled+disabled

	// 1. Users
	go func() {
		defer summaryWg.Done()
		n, err := collector.CollectCount(apiUrl, token, "user.get", nil, zabbixApiRequest)
		nUsersErr = err
		if err == nil { nUsers = n }
	}()

	// 2. Hosts total + enabled + disabled (3 calls — kept serial within this goroutine
	//    because they share the hostsErr variable and are fast count-only queries)
	go func() {
		defer summaryWg.Done()
			// use CollectCount helper for host counts
			n, herr := collector.CollectCount(apiUrl, token, "host.get", nil, zabbixApiRequest)
			hostsErr = herr
			if herr == nil { nTotalHosts = n }
			n, herr = collector.CollectCount(apiUrl, token, "host.get", map[string]interface{}{"filter": map[string]interface{}{"status": 0}}, zabbixApiRequest)
			if herr == nil { nEnabledHosts = n } else if hostsErr == nil { hostsErr = herr }
			n, herr = collector.CollectCount(apiUrl, token, "host.get", map[string]interface{}{"filter": map[string]interface{}{"status": 1}}, zabbixApiRequest)
			if herr == nil { nDisabledHosts = n } else if hostsErr == nil { hostsErr = herr }
	}()

	// 3. Templates count
	go func() {
		defer summaryWg.Done()
		if cnt, err := collector.CollectCount(apiUrl, token, "template.get", nil, zabbixApiRequest); err == nil { templatesCount = fmt.Sprintf("%d", cnt) }
	}()

	// 4. Items total
	go func() {
		defer summaryWg.Done()
		if cnt, err := collector.CollectCount(apiUrl, token, "item.get", map[string]interface{}{"templated": false, "webitems": true}, zabbixApiRequest); err == nil {
			nItemsTotal = fmt.Sprintf("%d", cnt)
		}
	}()

	// 5. Items enabled + disabled (both fast count queries, serial within goroutine)
	go func() {
		defer summaryWg.Done()
		if cnt, err := collector.CollectCount(apiUrl, token, "item.get", map[string]interface{}{"monitored": true, "templated": false, "webitems": true, "filter": map[string]interface{}{"status": 0, "state": 0}}, zabbixApiRequest); err == nil {
			nItemsEnabled = fmt.Sprintf("%d", cnt)
		}
		if cnt, err := collector.CollectCount(apiUrl, token, "item.get", map[string]interface{}{"templated": false, "webitems": true, "filter": map[string]interface{}{"status": 1}}, zabbixApiRequest); err == nil {
			nItemsDisabled = fmt.Sprintf("%d", cnt)
		}
	}()

	summaryWg.Wait()
	if progressCb != nil { progressCb("progress.collecting_hosts") }
	if nUsersErr != nil { return "", nUsersErr }
	if hostsErr != nil { return "", hostsErr }
	if progressCb != nil { progressCb("progress.collecting_templates") }
	if progressCb != nil { progressCb("progress.collecting_items") }

	// Fetch only the 'Admin' account (avoid fetching all users)
	var usersList []map[string]interface{}
	hasDefaultAdmin := false
	if arr, err := collector.CollectRawList(apiUrl, token, "user.get", map[string]interface{}{
		"output": []string{"userid", "username", "name", "surname", "status", "disabled"},
		"filter": map[string]interface{}{"username": "Admin"},
	}, zabbixApiRequest); err == nil {
		for _, u := range arr {
			usersList = append(usersList, u)
			// determine if the user is enabled (best-effort: check common fields)
			enabled := true
			if s, ok := u["status"]; ok {
				switch v := s.(type) {
				case float64:
					if int(v) != 0 { enabled = false }
				case int:
					if v != 0 { enabled = false }
				case bool:
					if v { enabled = false }
				case string:
					sv := strings.TrimSpace(v)
					if sv == "1" || strings.EqualFold(sv, "true") || strings.EqualFold(sv, "disabled") { enabled = false }
				}
			}
			if d, ok := u["disabled"]; ok {
				switch v := d.(type) {
				case bool:
					if v { enabled = false }
				case float64:
					if int(v) != 0 { enabled = false }
				case int:
					if v != 0 { enabled = false }
				case string:
					dv := strings.TrimSpace(v)
					if dv == "1" || strings.EqualFold(dv, "true") || strings.EqualFold(dv, "disabled") { enabled = false }
				}
			}
			// mark default Admin only when username == Admin AND enabled
			if fmt.Sprintf("%v", u["username"]) == "Admin" && enabled {
				hasDefaultAdmin = true
			}
		}
	}

	// Test whether the default Admin password 'zabbix' is accepted (best-effort).
	// Only attempt this when Admin account exists and appears enabled (hasDefaultAdmin).
	adminDefaultPasswordValid := false
	if hasDefaultAdmin {
		if tok, terr := collector.Authenticate(apiUrl, "Admin", "zabbix", zabbixApiRequest); terr == nil {
			if strings.TrimSpace(tok) != "" {
				adminDefaultPasswordValid = true
			}
		} else {
			// non-fatal: just log at debug level
			log.Printf("[DEBUG] default-admin password test failed: %v", terr)
		}
	}

	// get NVPS (Required server performance, new values per second)
	if progressCb != nil { progressCb("progress.collecting_nvps") }
	nvps := "N/A"
	requiredHost := os.Getenv("ZABBIX_SERVER_HOSTID")
	if requiredHost == "" { requiredHost = "10084" }
	log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID=%s will be used for item.get", requiredHost)
	if item, err := getItemByKey(apiUrl, token, "zabbix[requiredperformance]", requiredHost); err == nil {
		if item != nil {
			log.Printf("[DEBUG] Found requiredperformance item: itemid=%v hostid=%v value_type=%v key=%v", item["itemid"], item["hostid"], item["value_type"], item["key_"])
			itemid := fmt.Sprintf("%v", item["itemid"])
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
					if f, ferr := strconv.ParseFloat(strings.TrimSpace(val), 64); ferr == nil {
						nvps = fmt.Sprintf("%.2f", f)
					} else {
						nvps = val
					}
				} else {
					nvps = "<span data-i18n=\"stats.no_history\"></span>"
				}
			} else {
				log.Printf("[ERROR] history.get failed for itemid %s: %v", itemid, err)
				nvps = "<span data-i18n=\"error.read_history\"></span>"
			}
		} else {
			nvps = "<span data-i18n=\"error.create_requiredperformance_key\"></span>"
		}
	} else {
		log.Printf("[ERROR] item.get failed for requiredperformance: %v", err)
		nvps = "<span data-i18n=\"error.search_item\"></span>"
	}


	if progressCb != nil { progressCb("progress.collecting_unsupported_items") }
	itemsRaw, err := collector.CollectRawList(apiUrl, token, "item.get", map[string]interface{}{
		"output": []string{"itemid","name","templateid","error","key_"},
		"filter": map[string]interface{}{ "state": 1 },
		"webitems": 1,
		"selectHosts": []string{"name","hostid"},
		"inherited": true,
	}, zabbixApiRequest)
	items := []map[string]interface{}{}
	if err == nil { items = itemsRaw }

	// Buscar nome do template real de cada item (usar templateid como chave)
	// Primeiro crio a lista única de templateids, para retornados nos itens não suportados
	templateFakeSet := map[string]struct{}{}
	for _, item := range items {
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
		cacheArr, err := collector.CollectRawList(apiUrl, token, "item.get", map[string]interface{}{
			"output": []string{"name", "key_", "templateid", "itemid"},
			"itemids": templateFakeIds,
			"selectHosts": []string{"name", "hostid"},
		}, zabbixApiRequest)
		if err == nil {
			for _, itemMap := range cacheArr {
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
	for _, item := range items {
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
		if tplArr, err := collector.CollectRawList(apiUrl, token, "template.get", map[string]interface{}{
			"output": []string{"templateid","name"},
			"templateids": tplIds,
		}, zabbixApiRequest); err == nil {
			for _, tplMap := range tplArr {
				if tplMap != nil {
					templateNames[fmt.Sprintf("%v", tplMap["templateid"])] = fmt.Sprintf("%v", tplMap["name"])
				}
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

	// ── Coletar Triggers em estado Unknown (state=1) ─────────────────────────
	if progressCb != nil { progressCb("progress.collecting_triggers") }
	type triggerHostRow struct {
		HostName  string
		HostID    string
		Count     int
		TopErrors string
	}
	var triggerUnknownRows []triggerHostRow
	totalTriggersUnknown := 0
	// additional maps to aggregate triggers by template
	hostTrigCountByID := map[string]int{}
	hostErrorsByID := map[string][]string{}
	hostIDToName := map[string]string{}
	trigArr, trigErr := collector.CollectRawList(apiUrl, token, "trigger.get", map[string]interface{}{
		"output":      []string{"triggerid", "description", "error"},
		"selectHosts": []string{"hostid", "name"},
		"filter":      map[string]interface{}{"state": 1},
		"monitored":   true,
	}, zabbixApiRequest)

	// helper: extract a short, human-friendly error from long trigger error texts.
	// Strategy: take the substring after the last ": " (common pattern in Zabbix
	// errors like "...): item is not supported.") and trim whitespace. If not
	// found, return the original trimmed string.
	shortError := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" || s == "<nil>" { return "" }
		if idx := strings.LastIndex(s, ": "); idx != -1 && idx+2 < len(s) {
			return strings.TrimSpace(s[idx+2:])
		}
		return s
	}
	if trigErr == nil {
		hostTrigCount := map[string]int{}
		hostErrors := map[string][]string{}
		hostIDMap := map[string]string{} // hostName -> hostid
		for _, raw := range trigArr {
			trig := raw
			hostsArr, _ := trig["hosts"].([]interface{})
			hostName := ""
			hostID := ""
			if len(hostsArr) > 0 {
				if hm, ok3 := hostsArr[0].(map[string]interface{}); ok3 {
					hostName = fmt.Sprintf("%v", hm["name"])
					hostID = fmt.Sprintf("%v", hm["hostid"])
				}
			}
			if hostName == "" { continue }
			hostTrigCount[hostName]++
			hostIDMap[hostName] = hostID
			// populate by-id maps for template aggregation
			if hostID != "" {
				hostTrigCountByID[hostID]++
				hostIDToName[hostID] = hostName
			}
			// Use only the explicit error field from the trigger.
			errMsg := strings.TrimSpace(fmt.Sprintf("%v", trig["error"]))
			if errMsg == "" || errMsg == "<nil>" { errMsg = "" }
			if errMsg != "" {
				se := shortError(errMsg)
				if se != "" {
					hostErrors[hostName] = append(hostErrors[hostName], se)
					if hostID != "" {
						hostErrorsByID[hostID] = append(hostErrorsByID[hostID], se)
					}
				}
			}
		}
		type trigKV struct{ Host string; Count int }
		trigKVList := []trigKV{}
		for h, c := range hostTrigCount {
			trigKVList = append(trigKVList, trigKV{h, c})
			totalTriggersUnknown += c
		}
		sort.Slice(trigKVList, func(i, j int) bool { return trigKVList[i].Count > trigKVList[j].Count })
		for _, tkv := range trigKVList {
			errs := hostErrors[tkv.Host]
			// count occurrences per error message
			errCount := map[string]int{}
			for _, e := range errs { errCount[e]++ }
			// build sorted list of errors by count
			type ec struct{ Msg string; C int }
			ecs := []ec{}
			for m, c := range errCount { ecs = append(ecs, ec{m, c}) }
			sort.Slice(ecs, func(i, j int) bool { return ecs[i].C > ecs[j].C })
			maxHostErrs := 3
			if len(ecs) > maxHostErrs { ecs = ecs[:maxHostErrs] }
			topParts := []string{}
			for _, e := range ecs { topParts = append(topParts, fmt.Sprintf("%s:%d", e.Msg, e.C)) }
			topErr := ""
			if len(topParts) > 0 { topErr = strings.Join(topParts, ", ") }
			triggerUnknownRows = append(triggerUnknownRows, triggerHostRow{tkv.Host, hostIDMap[tkv.Host], tkv.Count, topErr})
		}
	}
	_ = totalTriggersUnknown // used in recommendations section

	// ── Agrupar Triggers Unknown por TEMPLATE (reaproveitando coletas) ───────
	// Build per-template counts and top errors using a single host.get to fetch parent templates
	templateTriggerCounts := map[string]int{}
	templateErrorCounts := map[string]map[string]int{}
	if len(hostTrigCountByID) > 0 {
		hostIds := []string{}
		for hid := range hostTrigCountByID { hostIds = append(hostIds, hid) }
		// call host.get to fetch parent templates for these hosts
		if hostArr, err := collector.CollectRawList(apiUrl, token, "host.get", map[string]interface{}{
			"output": []string{"hostid", "name"},
			"hostids": hostIds,
			"selectParentTemplates": []string{"templateid", "name"},
		}, zabbixApiRequest); err == nil {
			hostTemplates := map[string][]string{} // hostid -> []template names
			for _, h := range hostArr {
				hid := fmt.Sprintf("%v", h["hostid"])
				pt := []string{}
				if parents, ok2 := h["parentTemplates"].([]interface{}); ok2 {
					for _, p := range parents {
						if pm, ok3 := p.(map[string]interface{}); ok3 {
							if name := fmt.Sprintf("%v", pm["name"]); name != "" {
								pt = append(pt, name)
							}
						}
					}
				}
				hostTemplates[hid] = pt
			}
			// aggregate counts
			for hid, cnt := range hostTrigCountByID {
				tpls := hostTemplates[hid]
				if len(tpls) == 0 {
					// fallback: use host name as a pseudo-template
					name := hostIDToName[hid]
					templateTriggerCounts[name] += cnt
					if errs := hostErrorsByID[hid]; len(errs) > 0 {
						if templateErrorCounts[name] == nil { templateErrorCounts[name] = map[string]int{} }
						for _, e := range errs { templateErrorCounts[name][e]++ }
					}
					continue
				}
				for _, tname := range tpls {
					templateTriggerCounts[tname] += cnt
					if errs := hostErrorsByID[hid]; len(errs) > 0 {
						if templateErrorCounts[tname] == nil { templateErrorCounts[tname] = map[string]int{} }
						for _, e := range errs { templateErrorCounts[tname][e]++ }
					}
				}
			}
		}
	}

	// Compute how many templates will be shown in the Template table (we cap the display)
	maxTemplatesDisplayed := 20
	templatesShownCount := 0
	if len(templateTriggerCounts) > 0 {
		tp := sortMap(templateTriggerCounts)
		templatesShownCount = len(tp)
		if templatesShownCount > maxTemplatesDisplayed { templatesShownCount = maxTemplatesDisplayed }
	}

	// Descrições moved to i18n locale files

	// --- HTML ---
	html += `<div class='zabbix-report-modern'>`
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
			// If caller passed i18n keys prefixed with "i18n:", emit data-i18n attributes
			if strings.HasPrefix(title, "i18n:") {
				// support optional args after key using pipe separator: i18n:key|arg1|arg2
				raw := strings.TrimPrefix(title, "i18n:")
				parts := strings.Split(raw, "|")
				key := parts[0]
				titleArgs := ""
				if len(parts) > 1 {
					titleArgs = strings.Join(parts[1:], "|")
				}
				// tip may also be i18n:key|arg
				if strings.HasPrefix(tip, "i18n:") {
					r := strings.TrimPrefix(tip, "i18n:")
					rparts := strings.Split(r, "|")
					tipKey := rparts[0]
					tipArgs := ""
					if len(rparts) > 1 { tipArgs = strings.Join(rparts[1:], "|") }
					if tipArgs != "" {
						return fmt.Sprintf("<%s><span class='title-text' data-i18n='%s' data-i18n-args='%s'></span> <span class='info-icon' tabindex='0'>%s<span class='info-tooltip' data-i18n='%s' data-i18n-args='%s'></span></span></%s>", tag, htmlpkg.EscapeString(key), htmlpkg.EscapeString(titleArgs), sv, htmlpkg.EscapeString(tipKey), htmlpkg.EscapeString(tipArgs), tag)
					}
					return fmt.Sprintf("<%s><span class='title-text' data-i18n='%s' data-i18n-args='%s'></span> <span class='info-icon' tabindex='0'>%s<span class='info-tooltip' data-i18n='%s'></span></span></%s>", tag, htmlpkg.EscapeString(key), htmlpkg.EscapeString(titleArgs), sv, htmlpkg.EscapeString(tipKey), tag)
				}
				if titleArgs != "" {
					return fmt.Sprintf("<%s><span class='title-text' data-i18n='%s' data-i18n-args='%s'></span> <span class='info-icon' tabindex='0'>%s<span class='info-tooltip'>%s</span></span></%s>", tag, htmlpkg.EscapeString(key), htmlpkg.EscapeString(titleArgs), sv, tipEsc, tag)
				}
				return fmt.Sprintf("<%s><span class='title-text' data-i18n='%s'></span> <span class='info-icon' tabindex='0'>%s<span class='info-tooltip'>%s</span></span></%s>", tag, htmlpkg.EscapeString(key), sv, tipEsc, tag)
			}
			// title is plain text but tip may still be an i18n key
			if strings.HasPrefix(tip, "i18n:") {
				r := strings.TrimPrefix(tip, "i18n:")
				rparts := strings.Split(r, "|")
				tipKey := rparts[0]
				tipArgs := ""
				if len(rparts) > 1 { tipArgs = strings.Join(rparts[1:], "|") }
				if tipArgs != "" {
					return fmt.Sprintf("<%s><span class='title-text'>%s</span> <span class='info-icon' tabindex='0'>%s<span class='info-tooltip' data-i18n='%s' data-i18n-args='%s'></span></span></%s>", tag, htmlpkg.EscapeString(title), sv, htmlpkg.EscapeString(tipKey), htmlpkg.EscapeString(tipArgs), tag)
				}
				return fmt.Sprintf("<%s><span class='title-text'>%s</span> <span class='info-icon' tabindex='0'>%s<span class='info-tooltip' data-i18n='%s'></span></span></%s>", tag, htmlpkg.EscapeString(title), sv, htmlpkg.EscapeString(tipKey), tag)
			}
			// default: emit escaped title and tooltip
			return fmt.Sprintf("<%s><span class='title-text'>%s</span> <span class='info-icon' tabindex='0'>%s<span class='info-tooltip'>%s</span></span></%s>", tag, htmlpkg.EscapeString(title), sv, tipEsc, tag)
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
	html += `<div style='font-size:14px;color:#1f2937;'><strong data-i18n='label_environment'></strong> ` + htmlpkg.EscapeString(ambienteUrl) + `</div>`
	html += `<div style='font-size:14px;color:#1f2937;'><strong data-i18n='label_version'></strong> ` + htmlpkg.EscapeString(verLabel) + `</div>`
	html += `</div>`
	html += `<div class='tabs-container'>`
	html += `<button class='tab-btn active' data-tab='tab-resumo' data-i18n='tabs.summary'></button>`
	html += `<button class='tab-btn' data-tab='tab-processos' data-i18n='tabs.server'></button>`
	html += `<button class='tab-btn' data-tab='tab-proxys' data-i18n='tabs.proxys'></button>`
	html += `<button class='tab-btn' data-tab='tab-items' data-i18n='tabs.items'></button>`
	html += `<button class='tab-btn' data-tab='tab-templates' data-i18n='tabs.templates'></button>`
	html += `<button class='tab-btn' data-tab='tab-triggers' data-i18n='tabs.triggers'></button>`
	html += `<button class='tab-btn' data-tab='tab-top' data-i18n='tabs.top'></button>`
	html += `<button class='tab-btn' data-tab='tab-usuarios' data-i18n='tabs.users'></button>`
	html += `<button class='tab-btn' data-tab='tab-recomendacoes' data-i18n='tabs.recommendations'></button>`
	html += `</div>`

	// Tab panels: resumo (visible), others hidden by default
	html += `<div id='tab-resumo' class='tab-panel' style='display:block;'>`
	html += `<h2 class='tab-print-title' data-i18n='tabs.summary'></h2>`
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.param'></th><th data-i18n='table.value'></th><th data-i18n='table.details'></th></tr></thead><tbody>`
	// Hosts
	html += `<tr><td data-i18n='summary.hosts_count'></td><td>` + formatInt(nTotalHosts) + `</td><td>` + formatInt(nEnabledHosts) + ` / ` + formatInt(nDisabledHosts) + `</td></tr>`
	// Templates
	html += `<tr><td data-i18n='summary.templates_count'></td><td>` + templatesCount + `</td><td></td></tr>`
	// Items
	html += `<tr><td data-i18n='summary.items_count'></td><td>` + formatMaybeNumber(nItemsTotal) + `</td><td>` + formatMaybeNumber(nItemsEnabled) + ` / ` + formatMaybeNumber(nItemsDisabled) + ` / ` + formatMaybeNumber(nItemsNaoSuportados) + `</td></tr>`
	// Proxys
	if progressCb != nil { progressCb("progress.collecting_proxies") }
	var proxies []map[string]interface{}
	if plist, perr := collector.CollectProxiesList(apiUrl, token, zabbixApiRequest); perr == nil && plist != nil {
		proxies = plist
	} else if perr != nil {
		log.Printf("[ERROR] proxy.get (list) failed: %v", perr)
	}
	proxyCount := len(proxies)
	html += `<tr><td data-i18n='summary.proxies_count'></td><td>` + formatInt(proxyCount) + `</td><td></td></tr>`
	// Usuários
	html += `<tr><td data-i18n='summary.users_count'></td><td>` + formatInt(nUsers) + `</td><td></td></tr>`
	// NVPS
	html += `<tr><td data-i18n='summary.required_performance'></td><td>` + nvps + `</td><td></td></tr>`
	html += `</tbody></table></div>`

	// descriptions for tooltip (lowercase key) - moved here so pollers section can reference it
	// Values were migrated to i18n keys; translations live in web/locales/*/messages.json
	procDesc := map[string]string{
		"agent poller": "procdesc.agent_poller",
		"alert manager": "procdesc.alert_manager",
		"alert syncer": "procdesc.alert_syncer",
		"alerter": "procdesc.alerter",
		"availability manager": "procdesc.availability_manager",
		"browser poller": "procdesc.browser_poller",
		"configuration syncer": "procdesc.configuration_syncer",
		"configuration syncer worker": "procdesc.configuration_syncer_worker",
		"connector manager": "procdesc.connector_manager",
		"connector worker": "procdesc.connector_worker",
		"discovery manager": "procdesc.discovery_manager",
		"discovery worker": "procdesc.discovery_worker",
		"escalator": "procdesc.escalator",
		"ha manager": "procdesc.ha_manager",
		"history poller": "procdesc.history_poller",
		"history syncer": "procdesc.history_syncer",
		"housekeeper": "procdesc.housekeeper",
		"http agent poller": "procdesc.http_agent_poller",
		"http poller": "procdesc.http_poller",
		"icmp pinger": "procdesc.icmp_pinger",
		"internal poller": "procdesc.internal_poller",
		"ipmi manager": "procdesc.ipmi_manager",
		"ipmi poller": "procdesc.ipmi_poller",
		"java poller": "procdesc.java_poller",
		"lld manager": "procdesc.lld_manager",
		"lld worker": "procdesc.lld_worker",
		"odbc poller": "procdesc.odbc_poller",
		"poller": "procdesc.poller",
		"preprocessing manager": "procdesc.preprocessing_manager",
		"preprocessing worker": "procdesc.preprocessing_worker",
		"proxy poller": "procdesc.proxy_poller",
		"proxy group manager": "procdesc.proxy_group_manager",
		"report manager": "procdesc.report_manager",
		"report writer": "procdesc.report_writer",
		"self-monitoring": "procdesc.self_monitoring",
		"service manager": "procdesc.service_manager",
		"snmp poller": "procdesc.snmp_poller",
		"snmp trapper": "procdesc.snmp_trapper",
		"task manager": "procdesc.task_manager",
		"timer": "procdesc.timer",
		"trapper": "procdesc.trapper",
		"trigger housekeeper": "procdesc.trigger_housekeeper",
		"unreachable poller": "procdesc.unreachable_poller",
		"vmware collector": "procdesc.vmware_collector",
		"data sender": "procdesc.data_sender",
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
	html += `<h4 style='margin:0 0 8px 0;' data-i18n='gauge.hosts_disabled'></h4>`
	html += `<canvas id='hosts-gauge' width='200' height='200' style='max-width:200px;' data-total='` + fmt.Sprintf("%d", nTotalHosts) + `' data-unsupported='` + fmt.Sprintf("%d", nDisabledHosts) + `' data-unsupported-label='' data-supported-label='' data-color-unsupported='#ffcc66' data-color-supported='#66c2a5'></canvas>`
	// legend lines: color swatches and separated lines (supported / disabled)
	hostDisabledPct := 0.0
	if nTotalHosts > 0 { hostDisabledPct = (float64(nDisabledHosts) * 100.0) / float64(nTotalHosts) }
	html += `<div class='gauge-legend' style='width:100%;margin-top:8px;font-size:0.95em;'>`
	html += `<div style='display:flex;align-items:center;gap:8px;margin-bottom:6px;'><span style='display:inline-block;width:12px;height:12px;background:#66c2a5;border-radius:3px;'></span><strong data-i18n='gauge.total_hosts'></strong>&nbsp;` + formatInt(nTotalHosts) + `</div>`
	html += `<div style='display:flex;align-items:center;gap:8px;'><span style='display:inline-block;width:12px;height:12px;background:#ffcc66;border-radius:3px;'></span><strong data-i18n='gauge.disabled'></strong>&nbsp;` + formatInt(nDisabledHosts) + ` (` + fmt.Sprintf("%.2f", hostDisabledPct) + `%)</div>`
	html += `</div>`
	html += `</div>`
	// Items gauge (right)
	html += `<div class='card' style='background:#fff;color:#222;padding:12px;border-radius:8px;min-width:220px;box-shadow:0 1px 6px rgba(0,0,0,0.04);'>`
	html += `<h4 style='margin:0 0 8px 0;' data-i18n='gauge.items_unsupported'></h4>`
	html += `<canvas id='items-gauge' width='200' height='200' style='max-width:200px;' data-total='` + fmt.Sprintf("%d", totalItemsVal) + `' data-unsupported='` + fmt.Sprintf("%d", unsupportedVal) + `' data-unsupported-label='' data-supported-label='' data-color-unsupported='#ff7a7a' data-color-supported='#66c2a5'></canvas>`
	// legend lines for items
	itemsUnsupportedPct := 0.0
	if totalItemsVal > 0 { itemsUnsupportedPct = (float64(unsupportedVal) * 100.0) / float64(totalItemsVal) }
	html += `<div class='gauge-legend' style='width:100%;margin-top:8px;font-size:0.95em;'>`
	html += `<div style='display:flex;align-items:center;gap:8px;margin-bottom:6px;'><span style='display:inline-block;width:12px;height:12px;background:#66c2a5;border-radius:3px;'></span><strong data-i18n='gauge.total_items'></strong>&nbsp;` + formatInt(totalItemsVal) + `</div>`
	html += `<div style='display:flex;align-items:center;gap:8px;'><span style='display:inline-block;width:12px;height:12px;background:#ff7a7a;border-radius:3px;'></span><strong data-i18n='gauge.unsupported'></strong>&nbsp;` + formatInt(unsupportedVal) + ` (` + fmt.Sprintf("%.2f", itemsUnsupportedPct) + `%)</div>`
	html += `</div>`
	html += `</div>`
	html += `</div>`


	html += `</div>` // end tab-resumo

	// --- Processos e Threads Zabbix Server (Pollers + Internal) ---
		if progressCb != nil { progressCb("progress.collecting_pollers_processes") }
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
		html += `<h2 class='tab-print-title' data-i18n='tabs.server'></h2>`
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
	// Para Zabbix < 7, removemos a lista de processos que não existem nessa versão — em vez de exibi-los como desabilitados.
	if majorV < 7 {
		v6skip := map[string]bool{
			"configuration syncer worker": true,
			"lld manager":                 true,
			"lld worker":                  true,
		}
		filtered := procNames[:0:0]
		for _, n := range procNames {
			if !v6skip[strings.ToLower(strings.TrimSpace(n))] {
				filtered = append(filtered, n)
			}
		}
		procNames = filtered
	}
	// Single bulk item.get for ALL server process items (pollers + internal processes)
	allServerNames := append(append([]string{}, pollerNames...), procNames...)
	serverItemsMap, serverItemsErr := collector.CollectProcessItemsBulk(apiUrl, token, allServerNames, serverHost, zabbixApiRequest)
	if serverItemsErr != nil {
		log.Printf("[ERROR] bulk process item.get failed: %v", serverItemsErr)
		serverItemsMap = map[string]map[string]interface{}{}
	}
	log.Printf("[DEBUG] bulk process item.get: %d matches for %d names", len(serverItemsMap), len(allServerNames))
	// Check host existence once — reused in DisabledMsg across both goroutine loops
	serverHostExists := false
	if serverHost != "" {
		hostParams := map[string]interface{}{"output": []string{"hostid"}, "hostids": []string{serverHost}}
		if arr, err := collector.CollectRawList(apiUrl, token, "host.get", hostParams, zabbixApiRequest); err == nil {
			if len(arr) > 0 { serverHostExists = true }
		}
	}
		html += titleWithInfo("h3", "i18n:section.pollers", "i18n:tip.pollers|"+checkTrendDisplay)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.process'></th><th data-i18n='table.value_min'></th><th data-i18n='table.value_avg'></th><th data-i18n='table.value_max'></th><th data-i18n='table.status'></th></tr></thead><tbody>`
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
	// ── Phase 1: resolver metadados dos pollers a partir de serverItemsMap (sem chamadas à API) ──
	// A busca de items já foi feita em bulk acima; aqui apenas mapeamos nome→row e coletamos itemids.
	type serverItemRef struct{ slice string; idx int; itemid string; vtype int }
	var serverItemRefs []serverItemRef
	pollRows := []pollRow{}
	for idx, name := range pollerNames {
		baseName := strings.ToLower(strings.TrimSpace(name))
		descKey := procDesc[baseName]
		if descKey == "" { descKey = "procdesc.poller_process" }
		words := strings.Fields(name)
		for i, w := range words { tw := strings.TrimSpace(w); if len(tw) > 0 { words[i] = strings.ToUpper(tw[:1]) + strings.ToLower(tw[1:]) } }
		friendly := strings.Join(words, " ")
		pr := pollRow{Friendly: friendly, Desc: descKey, Disabled: false, Err: false, Vmax: -1, Vavg: -1}
		item := serverItemsMap[baseName]
		if item == nil {
			pr.Disabled = true
			if serverHost != "" && !serverHostExists {
				pr.DisabledMsg = `<span data-i18n='error.hostid_not_found' data-i18n-args='` + htmlpkg.EscapeString(serverHost) + `'></span>`
			} else if majorV < 7 {
				switch baseName {
				case "agent poller", "browser poller", "http agent poller", "snmp poller", "configuration syncer worker":
					pr.DisabledMsg = "<span data-i18n='process.not_in_version'></span>"
				default:
					pr.DisabledMsg = "<span data-i18n='process.disabled'></span>"
				}
			} else {
				pr.DisabledMsg = "<span data-i18n='process.disabled'></span>"
			}
		} else {
			iid := fmt.Sprintf("%v", item["itemid"])
			vt := 0
			if fmt.Sprintf("%v", item["value_type"]) == "3" { vt = 3 }
			serverItemRefs = append(serverItemRefs, serverItemRef{slice: "poll", idx: idx, itemid: iid, vtype: vt})
		}
		pollRows = append(pollRows, pr)
	}
	// render pollRows via closure — chamada após Phase 3 (stats corretos)
	renderPollRows := func() {
		for _, pr := range pollRows {
			nameCell := `<td class='proc-name-cell'>` +
				`<div class='proc-name-wrap'>` +
				`<span>` + pr.Friendly + `</span>` +
				`<span class='info-icon' tabindex='0'>` +
				`<svg class='info-svg-q' viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='2' fill='white'/><text x='8' y='12' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
				`<span class='info-tooltip' data-i18n='` + htmlpkg.EscapeString(pr.Desc) + `'></span>` +
				`</span>` +
				`</div></td>`
			if pr.Err {
				html += `<tr>` + nameCell + `<td colspan='4' data-i18n='error.fetch_data'></td></tr>`
				continue
			}
			if pr.Disabled {
				dm := "<span data-i18n='process.disabled'></span>"
				if pr.DisabledMsg != "" { dm = pr.DisabledMsg }
				html += `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td style='background:#cccccc;color:#000;padding:6px;border-radius:4px;text-align:center;'>` + dm + `</td></tr>`
				continue
			}
			html += `<tr>` + nameCell + `<td>` + pr.Smin + `</td><td>` + pr.Savg + `</td><td>` + pr.Smax + `</td><td style='` + pr.StatusStyle + `'>` + pr.StatusText + `</td></tr>`
		}
	}
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
	// ── Phase 1 (procs): resolver metadados dos processos internos (sem chamadas à API) ──
	procRows := []procRow{}
	for i, name := range procNames {
		words := strings.Fields(strings.TrimSpace(name))
		for wi, w := range words { tw := strings.TrimSpace(w); if len(tw) > 0 { words[wi] = strings.ToUpper(tw[:1]) + strings.ToLower(tw[1:]) } }
		if len(words) > 0 && strings.ToLower(words[0]) == "lld" { words[0] = "LLD" }
		friendly := strings.Join(words, " ") + " Internal Processes"
		baseName := strings.ToLower(strings.TrimSpace(name))
		descKey := procDesc[baseName]
		if descKey == "" { descKey = "procdesc.internal_process" }
		pr := procRow{Friendly: friendly, Desc: descKey, Disabled: false, Err: false, Vmax: -1, Vavg: -1}
		item := serverItemsMap[baseName]
		if item == nil {
			pr.Disabled = true
			if serverHost != "" && !serverHostExists {
				pr.DisabledMsg = `<span data-i18n='error.hostid_not_found' data-i18n-args='` + htmlpkg.EscapeString(serverHost) + `'></span>`
			} else {
				pr.DisabledMsg = "<span data-i18n='process.disabled'></span>"
			}
		} else {
			iid := fmt.Sprintf("%v", item["itemid"])
			vt := 0
			if fmt.Sprintf("%v", item["value_type"]) == "3" { vt = 3 }
			serverItemRefs = append(serverItemRefs, serverItemRef{slice: "proc", idx: i, itemid: iid, vtype: vt})
		}
		procRows = append(procRows, pr)
	}

	// ── Phase 2: um único trend.get para TODOS os itens do Server (pollers + internos) ──
	// Substitui ~N chamadas individuais getLastTrend por 1 getTrendsBulkStats + 1 getHistoryStatsBulkByType.
	srvIids := make([]string, 0, len(serverItemRefs))
	srvVtypes := map[string]int{}
	for _, ref := range serverItemRefs {
		srvIids = append(srvIids, ref.itemid)
		srvVtypes[ref.itemid] = ref.vtype
	}
	srvTrendMap, _ := getTrendsBulkStats(apiUrl, token, srvIids)
	if srvTrendMap == nil { srvTrendMap = map[string]map[string]interface{}{} }
	// Fallback: history.get bulk para itens sem dados de trend
	missingSrvH := map[string]int{}
	for _, iid := range srvIids {
		if _, ok := srvTrendMap[iid]; !ok { missingSrvH[iid] = srvVtypes[iid] }
	}
	if len(missingSrvH) > 0 {
		histSrv, _ := getHistoryStatsBulkByType(apiUrl, token, missingSrvH)
		if histSrv != nil {
			for iid, stats := range histSrv {
				if _, exists := srvTrendMap[iid]; !exists { srvTrendMap[iid] = stats }
			}
		}
	}

	// ── Phase 3: popular stats nas rows (pollRows e procRows) ──
	parseSrvStat := func(iid string) (smin, savg, smax string, vavg, vmx float64, stText, stStyle string) {
		vavg, vmx = -1, -1
		smin, savg, smax = "-", "-", "-"
		stText = "-"
		tr := srvTrendMap[iid]
		if tr == nil { return }
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
		vmin := pv("value_min")
		vavg = pv("value_avg")
		vmx = pv("value_max")
		smin, savg, smax = fv(vmin), fv(vavg), fv(vmx)
		if vavg >= 0 {
			if vavg < 59.9 {
				stText = "OK"; stStyle = "background:#66c28a;color:#000;padding:6px;border-radius:4px;text-align:center;"
			} else {
				stText = "Atenção"; stStyle = "background:#ff6666;color:#000;padding:6px;border-radius:4px;text-align:center;"
			}
		}
		return
	}
	for _, ref := range serverItemRefs {
		smin, savg, smax, vavg, vmx, stText, stStyle := parseSrvStat(ref.itemid)
		if ref.slice == "poll" {
			pr := &pollRows[ref.idx]
			pr.Smin, pr.Savg, pr.Smax = smin, savg, smax
			pr.Vavg, pr.Vmax = vavg, vmx
			pr.StatusText, pr.StatusStyle = stText, stStyle
			if vavg < 0 { pr.Disabled = true; pr.DisabledMsg = "<span data-i18n='process.disabled'></span>" }
		} else {
			pr := &procRows[ref.idx]
			pr.Smin, pr.Savg, pr.Smax = smin, savg, smax
			pr.Vavg, pr.Vmax = vavg, vmx
			pr.StatusText, pr.StatusStyle = stText, stStyle
			if vavg < 0 { pr.Disabled = true; pr.DisabledMsg = "<span data-i18n='process.disabled'></span>" }
		}
	}
	// Ordena por Vavg desc (rows desabilitadas com Vavg=-1 vão para o fim)
	sort.Slice(pollRows, func(i, j int) bool { return pollRows[i].Vavg > pollRows[j].Vavg })
	sort.Slice(procRows, func(i, j int) bool { return procRows[i].Vavg > procRows[j].Vavg })
	// Render pollRows com stats corretos (Phase 3 já atualizou as rows)
	renderPollRows()
	html += `</tbody></table></div>`
	html += titleWithInfo("h3", "i18n:section.internal_process", "i18n:tip.internal_process|"+checkTrendDisplay)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.process'></th><th data-i18n='table.value_min'></th><th data-i18n='table.value_avg'></th><th data-i18n='table.value_max'></th><th data-i18n='table.status'></th></tr></thead><tbody>`
	// render
	for _, pr := range procRows {
		nameCell := `<td class='proc-name-cell'>` +
		`<div class='proc-name-wrap'>` +
		`<span>` + pr.Friendly + `</span>` +
		`<span class='info-icon' tabindex='0'>` +
		`<svg class='info-svg-q' viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='2' fill='white'/><text x='8' y='12' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
		`<span class='info-tooltip' data-i18n='` + htmlpkg.EscapeString(pr.Desc) + `'></span>` +
		`</span>` +
		`</div></td>`
		if pr.Err {
			html += `<tr>` + nameCell + `<td colspan='4' data-i18n='error.fetch_data'></td></tr>`
			continue
		}
			       if pr.Disabled {
				       dm := "<span data-i18n='process.disabled'></span>"
				       if pr.DisabledMsg != "" { dm = pr.DisabledMsg }
				       html += `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td style='background:#cccccc;color:#000;padding:6px;border-radius:4px;text-align:center;'>` + dm + `</td></tr>`
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
	html += `<h2 class='tab-print-title' data-i18n='tabs.proxys'></h2>`
	html += titleWithInfo("h3", "i18n:section.proxies_summary", "i18n:tip.proxies_summary")
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
	html += `<div class='table-responsive'><table class='modern-table'><colgroup><col style='width:75%'><col style='width:25%'></colgroup><thead><tr><th data-i18n='table.description'></th><th data-i18n='table.quantity'></th></tr></thead><tbody>`
	unknownTdStyle := ""
	if unknown > 0 { unknownTdStyle = "background:#ff6666 !important;color:#000 !important;" }
	offlineTdStyle := ""
	if offline > 0 { offlineTdStyle = "background:#ff6666 !important;color:#000 !important;" }
	activeTdStyle := ""
	if active > 0 { activeTdStyle = "background:#66c28a !important;color:#000 !important;" }
	passiveTdStyle := ""
	if passive > 0 { passiveTdStyle = "background:#ffe08a !important;color:#000 !important;" }
	html += `<tr><td style='` + unknownTdStyle + `' data-i18n='proxy.unknown'></td><td style='` + unknownTdStyle + `'>` + formatInt(unknown) + `</td></tr>`
	html += `<tr><td style='` + offlineTdStyle + `' data-i18n='proxy.offline'></td><td style='` + offlineTdStyle + `'>` + formatInt(offline) + `</td></tr>`
	html += `<tr><td style='` + activeTdStyle + `' data-i18n='proxy.active'></td><td style='` + activeTdStyle + `'>` + formatInt(active) + `</td></tr>`
	html += `<tr><td style='` + passiveTdStyle + `' data-i18n='proxy.passive'></td><td style='` + passiveTdStyle + `'>` + formatInt(passive) + `</td></tr>`
	html += `<tr><td data-i18n='proxy.total'></td><td>` + formatInt(total) + ` &nbsp; <a href='` + ambienteUrl + `/zabbix.php?action=proxy.list&filter_rst=1' target='_blank' data-i18n='open_proxies_list'></a></td></tr>`
	html += `</tbody></table></div>`

	// Proxys details table (list)
	// show all proxies in the details list, with Status column based on state
	// map to store hostid discovered from the initial item.get (keyed by proxyid)
	proxyHostIdMap := make(map[string]string)
	var proxyHostIdMu sync.Mutex
	if len(proxies) > 0 {
		html += `<h4 data-i18n='section.proxies'></h4>`
		// Render table header conditionally: show Compatibility column only on Zabbix 7+
		html += `<div class='table-responsive'><table class='modern-table'>`
		if majorV >= 7 {
			html += `<colgroup><col style='width:36%'><col style='width:9%'><col style='width:11%'><col style='width:11%'><col style='width:11%'><col style='width:11%'><col style='width:11%'></colgroup><thead><tr><th data-i18n='proxy.name'></th><th data-i18n='proxy.type'></th><th data-i18n='proxy.total_items'></th><th data-i18n='proxy.items_unsupported'></th><th data-i18n='proxy.queue_10m'></th><th data-i18n='proxy.compatibility'></th><th data-i18n='table.status'></th></tr></thead><tbody>`
		} else {
			// Zabbix <7: do not include compatibility column
			html += `<colgroup><col style='width:46%'><col style='width:9%'><col style='width:13%'><col style='width:13%'><col style='width:19%'></colgroup><thead><tr><th data-i18n='proxy.name'></th><th data-i18n='proxy.type'></th><th data-i18n='proxy.total_items'></th><th data-i18n='proxy.items_unsupported'></th><th data-i18n='proxy.queue_10m'></th><th data-i18n='table.status'></th></tr></thead><tbody>`
		}
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

					if itemsArr, ierr := collector.CollectRawList(apiUrl, token, "item.get", paramsItems, zabbixApiRequest); ierr == nil {
						for _, m := range itemsArr {
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
							// capture hostid ONLY from process utilization items (key starts with "process.")
							if strings.HasPrefix(key, "process.") {
								if hidRaw, okhid := m["hostid"]; okhid {
									hid := fmt.Sprintf("%v", hidRaw)
									if hid != "" && hid != "<nil>" {
										proxyHostIdMu.Lock()
										if _, exists := proxyHostIdMap[proxyid]; !exists {
											proxyHostIdMap[proxyid] = hid
										}
										proxyHostIdMu.Unlock()
									}
								}
							}
						}
					}

					paramsTotal := map[string]interface{}{
						"output": "extend",
						"templated": false,
						"countOutput": true,
						"proxyids": proxyid,
					}
					if cnt, terr := collector.CollectCount(apiUrl, token, "item.get", paramsTotal, zabbixApiRequest); terr == nil {
						totalItemsVal = fmt.Sprintf("%d", cnt)
					}

					// compatibility — read from proxy record (returned by proxy.get)
					compatRaw := fmt.Sprintf("%v", p["compatibility"])
					compCell := "<td style='text-align:center;'>-</td>"
					switch compatRaw {
					case "", "<nil>", "0":
						compCell = "<td style='background:#cccccc;color:#000;padding:4px 6px;border-radius:4px;text-align:center;'><span data-i18n='compatibility.undefined'></span></td>"
					case "1":
						compCell = "<td style='background:#66c28a;color:#000;padding:4px 6px;border-radius:4px;text-align:center;'><span data-i18n='compatibility.current'></span></td>"
					case "2":
						compCell = "<td style='background:#ffe08a;color:#000;padding:4px 6px;border-radius:4px;text-align:center;'><span data-i18n='compatibility.outdated'></span></td>"
					case "3":
						compCell = "<td style='background:#ff6666;color:#000;padding:4px 6px;border-radius:4px;text-align:center;'><span data-i18n='compatibility.unsupported'></span></td>"
					default:
						compCell = fmt.Sprintf("<td style='text-align:center;'>%s</td>", htmlpkg.EscapeString(compatRaw))
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
					itemsUnsupportedVal = `-<span class='info-icon info-icon-sm' tabindex='0'>` +
						`<svg class='info-svg-w' viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#e6a817' stroke-width='1.6' fill='white'/><text x='8' y='11' text-anchor='middle' font-size='10' fill='#e6a817' font-family='Arial' font-weight='bold'>!</text></svg>` +
						`<span class='info-tooltip info-tooltip-left' data-i18n='tip.create_unsupported_key'></span></span>`
				}

				// Build row HTML; include compatibility column only for Zabbix 7+
				totalDisp := formatMaybeNumber(totalItemsVal)
				unsupportedDisp := formatMaybeNumber(itemsUnsupportedVal)
				queueDisp := formatMaybeNumber(queueVal)
				rowHTML := `<tr data-proxyid='` + htmlpkg.EscapeString(proxyid) + `'><td>` + htmlpkg.EscapeString(name) + `</td><td>` + htmlpkg.EscapeString(tipo) + `</td><td style='text-align:center;'>` + totalDisp + `</td><td style='text-align:center;'>` + unsupportedDisp + `</td><td style='text-align:center;'>` + queueDisp + `</td>`
				if majorV >= 7 {
					rowHTML += compCell
				}
				rowHTML += `<td style='` + statusStyle + `'>` + statusLabel + `</td></tr>`
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
		html += `<div class='como-corrigir' data-i18n='no_proxy_info'></div>`
	}

	html += titleWithInfo("h3", "i18n:section.proxy_processes", "i18n:tip.proxy_processes|"+checkTrendDisplay)
	if progressCb != nil { progressCb("progress.collecting_proxy_processes") }

	// All process names for proxy (pollers + internal merged into one table)
	proxyAllProcNames := []string{
		"data sender",
		"poller",
		"unreachable poller",
		"http poller",
		"icmp pinger",
		"ipmi poller",
		"java poller",
		"odbc poller",
		"trapper",
		"preprocessing manager",
		"preprocessing worker",
		"configuration syncer",
		"availability manager",
		"discovery manager",
		"discovery worker",
		"history syncer",
		"housekeeper",
		"ipmi manager",
		"lld manager",
		"lld worker",
		"task manager",
		"vmware collector",
		"self-monitoring",
	}
	if majorV >= 7 {
		proxyAllProcNames = append([]string{"agent poller", "browser poller", "http agent poller", "snmp poller"}, proxyAllProcNames...)
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
		HostId   string // hostid discovered from earlier item.get (if available)
	}
	var proxyMetaList []proxyMetaP
	// track proxies with compatibility issues (2=outdated, 3=unsupported)
	var proxyCompatOutdated []string
	var proxyCompatUnsupported []string
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
		// try to reuse hostid discovered earlier from the proxy-level item.get
		hostFromMap := ""
		proxyHostIdMu.Lock()
		if v, ok := proxyHostIdMap[pid]; ok {
			hostFromMap = v
		}
		proxyHostIdMu.Unlock()
		// detect compatibility issues reported in the proxy record
		compatRaw2 := fmt.Sprintf("%v", p["compatibility"])
		if compatRaw2 == "2" {
			proxyCompatOutdated = append(proxyCompatOutdated, nm)
		} else if compatRaw2 == "3" {
			proxyCompatUnsupported = append(proxyCompatUnsupported, nm)
		}
		proxyMetaList = append(proxyMetaList, proxyMetaP{Idx: i, ProxyId: pid, Name: nm, Online: effState == "2", EffState: effState, HostId: hostFromMap})
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
			// If we discovered a hostid earlier when querying items by proxyid, reuse it first
			if pm.HostId != "" {
				hostId = pm.HostId
			}
			// Attempt A: Zabbix 6 — proxyid IS the hostid
			// Only try this if we don't already have a hostId discovered from items
			if hostId == "" {
				if arr, err := collector.CollectRawList(apiUrl, token, "host.get", map[string]interface{}{
					"output":  []string{"hostid"},
					"hostids": []string{pm.ProxyId},
				}, zabbixApiRequest); err == nil {
					if len(arr) > 0 {
						hostId = pm.ProxyId
					}
				}
			}
			// Attempt B: Zabbix 7 — match by technical hostname (host field)
			if hostId == "" {
				if arr, err := collector.CollectRawList(apiUrl, token, "host.get", map[string]interface{}{
					"output": []string{"hostid"},
					"filter": map[string]interface{}{"host": pm.Name},
				}, zabbixApiRequest); err == nil {
					if len(arr) > 0 {
						if hm := arr[0]; hm != nil {
							hostId = fmt.Sprintf("%v", hm["hostid"])
							log.Printf("[DEBUG] proxy '%s': found by technical name → hostid=%s", pm.Name, hostId)
						}
					}
				}
			}
			// Attempt C: Zabbix 7 — match by display name (name field, exact)
			if hostId == "" {
				if arr, err := collector.CollectRawList(apiUrl, token, "host.get", map[string]interface{}{
					"output": []string{"hostid"},
					"filter": map[string]interface{}{"name": pm.Name},
				}, zabbixApiRequest); err == nil {
					if len(arr) > 0 {
						if hm := arr[0]; hm != nil {
							hostId = fmt.Sprintf("%v", hm["hostid"])
							log.Printf("[DEBUG] proxy '%s': found by display name → hostid=%s", pm.Name, hostId)
						}
					}
				}
			}
			// If still not resolved, probe via item.get by proxyid searching specifically for
			// process utilization items (key_ starting with "process.") — these only exist on
			// the proxy's own self-monitoring host, so the hostid returned is always correct.
			if hostId == "" {
				probeParams := map[string]interface{}{
					"output":                 []string{"hostid", "key_"},
					"proxyids":               pm.ProxyId,
					"filter":                 map[string]interface{}{"type": []int{5, 18}},
					"search":                 map[string]interface{}{"key_": "process."},
					"searchWildcardsEnabled": false,
				}
				if arr, err := collector.CollectRawList(apiUrl, token, "item.get", probeParams, zabbixApiRequest); err == nil {
					if len(arr) > 0 {
						if im := arr[0]; im != nil {
							hostId = fmt.Sprintf("%v", im["hostid"])
						}
					}
				}
			}
			if hostId == "" {
				hostId = pm.ProxyId
			}

			// ── Step 2: single item.get for all process names on this proxy's host ──
			// Uses getProxyProcessItems: fetches ALL type=5 (Zabbix internal) items and
			// matches client-side on BOTH key_ AND name — robust across Zabbix 6/7 key formats.
			itemsMap, iErr := collector.CollectProxyProcessItems(apiUrl, token, proxyAllProcNames, hostId, zabbixApiRequest)
			if iErr != nil {
				log.Printf("[ERROR] proxy '%s' item.get failed: %v", pm.Name, iErr)
				res.noItemsNote = fmt.Sprintf("Erro ao consultar itens (hostid=%s).", htmlpkg.EscapeString(hostId))
				ppCh <- res
				return
			}
			if len(itemsMap) == 0 {
				res.noItemsNote = `<span data-i18n='proxy.no_items_note'></span>`
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
				// proxyAllProcNames now uses spaces as separators (like pollerNames),
				// so baseName == dispBaseName; no * replacement needed.
				baseName := strings.ToLower(strings.TrimSpace(procName)) // key for itemsMap lookup
				displayName := procName                                   // already space-separated
				dispBaseName := baseName
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
				descKey := procDesc[dispBaseName]
				if descKey == "" { descKey = "procdesc.process" }

				nameCell := `<td class='proc-name-cell'>` +
					`<div class='proc-name-wrap'><span>` + htmlpkg.EscapeString(friendly) + `</span>` +
					`<span class='info-icon' tabindex='0'>` +
					`<svg class='info-svg-q' viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='2' fill='white'/><text x='8' y='12' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
					`<span class='info-tooltip' data-i18n='` + htmlpkg.EscapeString(descKey) + `'></span></span></div></td>`

				item := itemsMap[baseName]
				if item == nil {
					res.rows = append(res.rows, proxyProcRow{friendly: friendly, vavg: -1,
						rowHTML: `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td class='status-na'>` +
							`-<span class='info-icon info-icon-sm' tabindex='0'>` +
							`<svg class='info-svg-w' viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#e6a817' stroke-width='1.6' fill='white'/><text x='8' y='11' text-anchor='middle' font-size='10' fill='#e6a817' font-family='Arial' font-weight='bold'>!</text></svg>` +
							`<span class='info-tooltip info-tooltip-left' data-i18n='tip.create_item_or_process_disabled'></span></span>` +
							`</td></tr>`})
					continue
				}
				iid := fmt.Sprintf("%v", item["itemid"])
				tr := trendMap[iid]
				if tr == nil {
					res.rows = append(res.rows, proxyProcRow{friendly: friendly, vavg: -1,
						rowHTML: `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td class='status-na'>Sem dados</td></tr>`})
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
	// Proxies online (Zabbix 7+) que não têm algum poller assíncrono habilitado
	// map[proxyName] -> []nomes de pollers ausentes
	proxyMissingAsyncMap := map[string][]string{}
	asyncProcNames := []string{"agent poller", "http agent poller", "snmp poller"}
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
		// Verifica pollers assíncronos somente em Zabbix 7+ com itens coletados
		if majorV >= 7 && res.noItemsNote == "" && len(res.rows) > 0 {
			// Constrói set dos friendly names coletados para lookup rápido
			collectedFriendly := map[string]bool{}
			for _, row := range res.rows {
				collectedFriendly[strings.ToLower(row.friendly)] = true
			}
			var missing []string
			for _, apn := range asyncProcNames {
				// O item existe mas disabled = vavg < 0 (sem dados / "Criar item...") → ausente
				// Procura na lista de rows pelo friendly correspondente
				found := false
				hasData := false
				for _, row := range res.rows {
					if strings.ToLower(row.friendly) == apn {
						found = true
						if row.vavg >= 0 { hasData = true }
						break
					}
				}
				if !found || !hasData {
					// Capitaliza como friendly name para exibição
					words := strings.Fields(apn)
					for i, w := range words { words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:]) }
					missing = append(missing, strings.Join(words, " "))
				}
			}
			if len(missing) > 0 {
				proxyMissingAsyncMap[pm.Name] = missing
			}
		}
	}

	if len(proxyMetaList) == 0 {
		html += `<div class='como-corrigir' data-i18n='no_proxy_configured'></div>`
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
				html += `<div style='padding:10px 14px;color:#888;' data-i18n='no_proxy_process_data'></div></details>`
				continue
			}
			html += `<div style='padding:8px 14px 12px;'>`
			if res.noItemsNote != "" {
				html += `<div class='como-corrigir' style='margin:4px 0 8px;'>` + res.noItemsNote + `</div>`
			}
			if len(res.rows) > 0 {
				html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.process'></th><th data-i18n='table.value_min'></th><th data-i18n='table.value_avg'></th><th data-i18n='table.value_max'></th><th data-i18n='table.status'></th></tr></thead><tbody>`
				for _, r := range res.rows { html += r.rowHTML }
				html += `</tbody></table></div>`
			}
			html += `</div></details>`
		}
		html += `</div>`
	}

	html += `</div>` // end tab-proxys

	// --- Items tab (Items não suportados + Intervalo de Coleta) ---
	if progressCb != nil { progressCb("progress.collecting_items_without_template_and_unsupported") }
	html += `<div id='tab-items' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title' data-i18n='tabs.items'></h2>`
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
	if c, err := collector.CollectCount(apiUrl, token, "item.get", paramsNoTpl, zabbixApiRequest); err == nil {
		itemsNoTplCount = c
	}
	// build link for items without template
	var itemsNoTplPath string
	if majorV >= 7 {
		itemsNoTplPath = "zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=0&filter_discovered=0&filter_set=1"
	} else {
		itemsNoTplPath = "items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=0&filter_discovered=0&filter_set=1"
	}
	itemsNoTplLink := ambienteUrl + "/" + itemsNoTplPath

	// Link used to open the full listing for unsupported items (reused)
	var unsupportedPath string
	if majorV >= 7 {
		unsupportedPath = "zabbix.php?action=item.list&context=host&filter_evaltype=0&filter_name=&filter_type=-1&filter_key=&filter_snmp_oid=&filter_value_type=-1&filter_delay=&filter_history=&filter_trends=&filter_status=-1&filter_state=1&filter_inherited=-1&filter_discovered=-1&filter_with_triggers=-1&filter_profile=web.hosts.items.list.filter&filter_tab=1&sort=name&sortorder=ASC"
	} else {
		unsupportedPath = "items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1"
	}
	unsupportedLink := ambienteUrl + "/" + unsupportedPath

	// Dedicated link for the "Items disabled" quick link (unique for that line)
	var itemsDisabledPath string
	if majorV >= 7 {
		itemsDisabledPath = "zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_status=1&filter_state=-1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1"
	} else {
		itemsDisabledPath = "items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_status=1&filter_state=-1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1"
	}
	itemsDisabledLink := ambienteUrl + "/" + itemsDisabledPath
	html += titleWithInfo("h3", "i18n:section.items_no_template", "i18n:tip.items_no_template")
	if itemsNoTplCount > 0 {
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.description'></th><th data-i18n='table.quantity'></th><th data-i18n='table.link'></th></tr></thead><tbody>`
			html += `<tr><td data-i18n='items.no_template'>Items sem Template</td><td>` + formatInt(itemsNoTplCount) + `</td><td><a href='` + itemsNoTplLink + `' target='_blank' data-i18n='open'></a></td></tr>`
		html += `</tbody></table></div>`
	} else {
		html += ``
	}

	html += titleWithInfo("h3", "i18n:section.items_unsupported", "i18n:tip.items_unsupported")
	// (legend use .como-corrigir)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='items.type'></th><th data-i18n='table.total'></th><th data-i18n='label_unsupported'></th><th data-i18n='table.link'></th></tr></thead><tbody>`

	// Define item types to query (type code -> label)
	baseTypes := []struct{ Code int; Label string }{
		{0, "<span data-i18n='types.zabbix_agent'></span>"},
		{2, "<span data-i18n='types.zabbix_trapper'></span>"},
		{3, "<span data-i18n='types.simple_check'></span>"},
		{5, "<span data-i18n='types.zabbix_internal'></span>"},
		{7, "<span data-i18n='types.zabbix_agent_active'></span>"},
		{8, "<span data-i18n='types.aggregate'></span>"},
		{9, "<span data-i18n='types.web_item'></span>"},
		{10, "<span data-i18n='types.external_check'></span>"},
		{11, "<span data-i18n='types.database'></span>"},
		{12, "<span data-i18n='types.ipmi'></span>"},
		{13, "<span data-i18n='types.ssh'></span>"},
		{14, "<span data-i18n='types.telnet'></span>"},
		{15, "<span data-i18n='types.calculated'></span>"},
		{16, "<span data-i18n='types.jmx'></span>"},
		{17, "<span data-i18n='types.snmp_trap'></span>"},
		{18, "<span data-i18n='types.dependent_item'></span>"},
		{19, "<span data-i18n='types.http_agent'></span>"},
		{20, "<span data-i18n='types.snmp'></span>"},
		{21, "<span data-i18n='types.script'></span>"},
	}
	types := baseTypes
	// Include Browser (22) only when Zabbix major version is 7 or newer
	if majorV >= 7 {
		types = append(types, struct{ Code int; Label string }{22, "<span data-i18n='types.browser'></span>"})
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

			if c, err := collector.CollectCount(apiUrl, token, "item.get", paramsTotal, zabbixApiRequest); err == nil {
				cntTotal = c
			}
			if c2, err2 := collector.CollectCount(apiUrl, token, "item.get", paramsUnsup, zabbixApiRequest); err2 == nil {
				cntUnsup = c2
			}

			perPath := strings.Replace(itemsPath, "filter_type=-1", fmt.Sprintf("filter_type=%d", tt.Code), 1)
			perLink := ambienteUrl + "/" + perPath
			linkHTML := ""
			if tt.Code == 22 && majorV < 7 {
				linkHTML = "-"
			} else {
				linkHTML = "<a href='" + perLink + "' target='_blank' data-i18n='open'></a>"
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
			html += `<tr><td>` + r.Label + `</td><td>` + formatInt(r.Total) + `</td><td>` + formatInt(r.Unsup) + `</td><td>` + r.Link + `</td></tr>`
	}

	// final total row: use existing total items query result for total items, and unsupportedVal for not supported total
	totalItemsInt := 0
	if nItemsTotal != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItemsTotal)); err == nil { totalItemsInt = v }
	}
	html += `<tr><td><strong data-i18n='strong.total'>Total</strong></td><td><strong>` + formatInt(totalItemsInt) + `</strong></td><td><strong>` + formatInt(unsupportedVal) + `</strong></td><td><a href='` + unsupportedLink + `' target='_blank' data-i18n='open_full_listing'></a></td></tr>`
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
		if c, err := collector.CollectCount(apiUrl, token, "item.get", params, zabbixApiRequest); err == nil {
			cnt = c
		}
		// montar link para a listagem com filter_delay (usar path apropriado para versão do Zabbix)
		var perPath string
		if majorV >= 7 {
			perPath = fmt.Sprintf("zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=%d&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_state=-1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1", d)
		} else {
			perPath = fmt.Sprintf("items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=%d&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1", d)
		}
		perLink := ambienteUrl + "/" + perPath
		linkHTML := "<a href='" + perLink + "' target='_blank' data-i18n='open'></a>"
		intervalRows = append(intervalRows, struct{ Interval int; Count int; Link string }{Interval: d, Count: cnt, Link: linkHTML})
	}

	// renderiza a seção de Intervalo de Coleta
	// (legend use .como-corrigir)
	html += titleWithInfo("h3", "i18n:section.collection_interval", "i18n:tip.collection_interval")
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.interval_seconds'></th><th data-i18n='table.quantity'></th><th data-i18n='table.link'></th></tr></thead><tbody>`
	for _, r := range intervalRows {
		if r.Count == 0 {
			continue
		}
			html += `<tr><td>` + formatInt(r.Interval) + `</td><td>` + formatInt(r.Count) + `</td><td>` + r.Link + `</td></tr>`
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
		if c, err := collector.CollectCount(apiUrl, token, "discoveryrule.get", params, zabbixApiRequest); err == nil {
			cnt = c
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
		linkHTML := "<a href='" + perLink + "' target='_blank' data-i18n='open'></a>"

		lldRows = append(lldRows, struct{ Interval int; Count int; Link string }{Interval: d, Count: cnt, Link: linkHTML})
	}

	// renderiza a seção de Regras de LLD (Intervalo de Coleta)
	html += titleWithInfo("h3", "i18n:section.lld_interval", "i18n:tip.lld_interval")
	// legend moved into tooltip via titleWithInfo
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.interval_seconds'></th><th data-i18n='table.quantity'></th><th data-i18n='table.link'></th></tr></thead><tbody>`
	for _, r := range lldRows {
		if r.Count == 0 {
			continue
		}
			html += `<tr><td>` + formatInt(r.Interval) + `</td><td>` + formatInt(r.Count) + `</td><td>` + r.Link + `</td></tr>`
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
	if c, err := collector.CollectCount(apiUrl, token, "discoveryrule.get", paramsNotSup, zabbixApiRequest); err == nil {
		lldNotSupCnt = c
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
		html += titleWithInfo("h3", "i18n:section.lld_not_supported", "i18n:tip.lld_not_supported")
		// legend moved into tooltip via titleWithInfo
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.description'></th><th data-i18n='table.quantity'></th><th data-i18n='table.link'></th></tr></thead><tbody>`
			html += `<tr><td data-i18n='lld.error_rules'>Regras de descoberta com status de erro</td><td>` + formatInt(lldNotSupCnt) + `</td><td><a href='` + lldPerLink + `' target='_blank' data-i18n='open'></a></td></tr>`
		html += `</tbody></table></div>`
	}

	// --- Items Texto com Histórico ---
	// Busca items do tipo Texto (value_type = 4) com intervalo de coleta menor ou igual a 300s
	html += titleWithInfo("h3", "i18n:section.text_items_history", "i18n:tip.text_items_history")

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
		"limit":      500,
		// include hosts info for each item
		"selectHosts": []string{"hostid"},
	}

	type textRowT struct{ Template string; Name string; ItemID string; Delay string; HostID string }
	textRows := []textRowT{}
	textCount := 0
	hostIDSet := map[string]bool{}
	if arr, err := collector.CollectRawList(apiUrl, token, "item.get", paramsTextItems, zabbixApiRequest); err == nil {
		for _, m := range arr {
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
		if tplArr, err := collector.CollectRawList(apiUrl, token, "template.get", paramsTpl, zabbixApiRequest); err == nil {
			for _, tm := range tplArr {
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

	if textCount > 0 {
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.template'></th><th data-i18n='items.item_name'></th><th data-i18n='table.itemid'></th><th data-i18n='table.interval_seconds'></th><th data-i18n='table.link'></th></tr></thead><tbody>`
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
					linkHTML = "<a href='" + ambienteUrl + "/" + perPath + "' target='_blank' data-i18n='open'></a>"
				} else {
					perPath := fmt.Sprintf("items.php?context=template&filter_hostids%%5B%%5D=%s&filter_name=%s&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_status=-1&filter_with_triggers=-1&filter_inherited=-1&filter_set=1", tplID, escName)
					linkHTML = "<a href='" + ambienteUrl + "/" + perPath + "' target='_blank' data-i18n='open'></a>"
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
	html += `<h2 class='tab-print-title' data-i18n='tabs.templates'></h2>`
	// Detalhamento dos Principais Templates
	html += titleWithInfo("h3", "i18n:section.templates_offenders", "i18n:tip.templates_offenders")
		// legend moved into tooltip via titleWithInfo
	for _, tpl := range topTemplates {
		tplName := templateNames[tpl.Key]
		if tplName == "" { tplName = tpl.Key }
		html += `<h4>` + htmlpkg.EscapeString(tplName) + `</h4>`
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.item'></th><th data-i18n='table.error'></th><th data-i18n='table.host'></th><th data-i18n='table.link'></th></tr></thead><tbody>`
		rows := templateItems[tpl.Key]
		for i, row := range rows {
			if i >= topN { break }
			html += `<tr><td>` + htmlpkg.EscapeString(row[0]) + `</td><td>` + htmlpkg.EscapeString(row[1]) + `</td><td>` + htmlpkg.EscapeString(row[2]) + `</td><td><a href='` + row[3] + `' target='_blank' data-i18n='edit'></a></td></tr>`
		}
		html += `</tbody></table></div>`
	}
	// close templates tab and main container
	html += `</div>` // end tab-templates

	// --- Top Templates/Items tab ---
	html += `<div id='tab-top' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title' data-i18n='tabs.top'></h2>`
	// Top Templates Ofensores
	html += titleWithInfo("h3", "i18n:section.top_templates", "i18n:tip.top_templates")
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.template'></th><th data-i18n='table.error_count'></th></tr></thead><tbody>`
	for _, tpl := range topTemplates {
		tplName := templateNames[tpl.Key]
		if tplName == "" { tplName = tpl.Key }
			html += `<tr><td>` + htmlpkg.EscapeString(tplName) + `</td><td>` + formatInt(tpl.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Montar o Top Hosts Ofensores (com template mais recorrente com itens problemáticos para cada host)
	html += titleWithInfo("h3", "i18n:section.top_hosts", "i18n:tip.top_hosts")
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.host'></th><th data-i18n='table.template'></th><th data-i18n='table.error_count'></th></tr></thead><tbody>`
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
			html += `<tr><td>` + htmlpkg.EscapeString(host.Key) + `</td><td>` + htmlpkg.EscapeString(mainTplName) + `</td><td>` + formatInt(host.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Top Items Problemáticos
	html += titleWithInfo("h3", "i18n:section.top_items", "i18n:tip.top_items")
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.item'></th><th data-i18n='table.template'></th><th data-i18n='table.error_count'></th></tr></thead><tbody>`
	for _, item := range topItems {
		parts := strings.SplitN(item.Key, "|", 2)
		itemName := parts[0]
		tplId := ""
		if len(parts) > 1 { tplId = parts[1] }
		tplName := templateNames[tplId]
		if tplName == "" { tplName = tplId }
			html += `<tr><td>` + htmlpkg.EscapeString(itemName) + `</td><td>` + htmlpkg.EscapeString(tplName) + `</td><td>` + formatInt(item.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Tipos de Erro Mais Comuns
	html += titleWithInfo("h3", "i18n:section.error_types", "i18n:tip.error_types")
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th data-i18n='table.error_message'></th><th data-i18n='table.template'></th><th data-i18n='table.occurrences'></th></tr></thead><tbody>`
	for _, errRow := range topErrors {
		parts := strings.SplitN(errRow.Key, "|", 2)
		errMsg := parts[0]
		tplId := ""
		if len(parts) > 1 { tplId = parts[1] }
		tplName := templateNames[tplId]
		if tplName == "" { tplName = tplId }
			html += `<tr><td>` + htmlpkg.EscapeString(errMsg) + `</td><td>` + htmlpkg.EscapeString(tplName) + `</td><td>` + formatInt(errRow.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`
	html += `</div>` // end tab-top

	// ── Tab: Usuários ─────────────────────────────────────────────────────────
	html += `<div id='tab-usuarios' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title' data-i18n='tabs.users'></h2>`
	html += titleWithInfo("h3", "i18n:section.users", "i18n:tip.users")

	// Security alert: default Admin found. Highlight as CRITICAL when default password 'zabbix' is accepted.
	if hasDefaultAdmin {
		if adminDefaultPasswordValid {
			html += `<div style='background:#fee2e2;border:1px solid #f43f5e;border-radius:8px;padding:12px 16px;margin-bottom:16px;display:flex;align-items:flex-start;gap:10px;'>` +
				`<div><strong data-i18n='users.default_admin_alert_title'></strong>` +
				`<p style='margin:4px 0 0;font-size:0.88em;color:#7f1d1d;' data-i18n='users.default_admin_alert_desc'></p>` +
				`<p style='margin:8px 0 0;font-size:0.95em;color:#b91c1c;font-weight:700;' data-i18n='fix.default_admin_password_in_use'></p></div>` +
				`</div>`
		} else {
			html += `<div style='background:#fff1f0;border:1px solid #fca5a5;border-radius:8px;padding:12px 16px;margin-bottom:16px;display:flex;align-items:flex-start;gap:10px;'>` +
				`<div><strong data-i18n='users.default_admin_alert_title'></strong>` +
				`<p style='margin:4px 0 0;font-size:0.88em;color:#7f1d1d;' data-i18n='users.default_admin_alert_desc'></p></div>` +
				`</div>`
		}
	}

	if len(usersList) == 0 {
		html += `<p data-i18n='users.no_data'></p>`
	} else {
		// Simple table showing the Admin account only (no counts, no type column)
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr>` +
			`<th data-i18n='table.username'></th>` +
			`<th data-i18n='table.fullname'></th>` +
			`<th data-i18n='table.default_password'></th>` +
			`</tr></thead><tbody>`
		for _, u := range usersList {
			username := fmt.Sprintf("%v", u["username"])
			firstName := fmt.Sprintf("%v", u["name"])
			surname := fmt.Sprintf("%v", u["surname"])
			fullName := strings.TrimSpace(firstName + " " + surname)
			rowStyle := ""
			if username == "Admin" {
				rowStyle = " style='background:#fff7ed;'"
			}
			// determine default-password badge for this user (only relevant for Admin)
			defaultPwdCell := "<td><span style='background:#ecfdf5;color:#065f46;border-radius:4px;padding:1px 6px;font-size:0.78rem;font-weight:700;'><span data-i18n='users.default_password_no'></span></span></td>"
			if username == "Admin" && adminDefaultPasswordValid {
				defaultPwdCell = "<td><span style='background:#fee2e2;color:#b91c1c;border-radius:4px;padding:1px 6px;font-size:0.78rem;font-weight:700;'><span data-i18n='users.default_password_yes'></span></span></td>"
			}

			html += `<tr` + rowStyle + `>` +
				`<td><strong>` + htmlpkg.EscapeString(username) + `</strong>` +
				func() string {
					if username == "Admin" { return ` <span style='background:#fee2e2;color:#b91c1c;border-radius:4px;padding:1px 5px;font-size:0.72rem;font-weight:700;margin-left:4px;' data-i18n='users.default_badge'></span>` }
					return ""
				}() +
				`</td>` +
				`<td>` + htmlpkg.EscapeString(fullName) + `</td>` +
				defaultPwdCell +
				`</tr>`
		}
		html += `</tbody></table></div>`
	}
	html += `</div>` // end tab-usuarios

	// ── Tab: Triggers Unknown ─────────────────────────────────────────────────
	html += `<div id='tab-triggers' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title' data-i18n='tabs.triggers'></h2>`

	if len(triggerUnknownRows) == 0 {
		html += `<p data-i18n='triggers.no_data'></p>`
	} else {
		// Render aggregated table: Triggers Unknown grouped by TEMPLATE
		if len(templateTriggerCounts) > 0 {
			// Title with info-icon for the Template table
			html += titleWithInfo("h3", "i18n:section.triggers_unknown_by_template", "i18n:tip.triggers_by_template")
			topTemplates := sortMap(templateTriggerCounts)
			maxTemplates := 20
			if len(topTemplates) > maxTemplates { topTemplates = topTemplates[:maxTemplates] }
			html += `<div class='table-responsive'><table class='modern-table'><thead><tr>` +
				` <th data-i18n='table.template'></th>` +
				` <th data-i18n='kpi.triggers_unknown'></th>` +
				` <th data-i18n='table.errors'></th>` +
				`</tr></thead><tbody>`
			for _, kv := range topTemplates {
				// collect top errors for this template
				errMap := templateErrorCounts[kv.Key]
				type ekv struct{ Key string; Value int }
				errArr := []ekv{}
				for e, c := range errMap { errArr = append(errArr, ekv{e, c}) }
				if len(errArr) > 0 {
					sort.Slice(errArr, func(i, j int) bool { return errArr[i].Value > errArr[j].Value })
				}
				topErrs := 5
				if len(errArr) > topErrs { errArr = errArr[:topErrs] }
				errParts := []string{}
				for _, e := range errArr { errParts = append(errParts, fmt.Sprintf("%s:%d", e.Key, e.Value)) }
				errsCell := "<no error>"
				if len(errParts) > 0 { errsCell = strings.Join(errParts, ", ") }
				html += `<tr>` +
					`<td>` + htmlpkg.EscapeString(kv.Key) + `</td>` +
					`<td>` + formatInt(kv.Value) + `</td>` +
					`<td>` + htmlpkg.EscapeString(errsCell) + `</td>` +
					`</tr>`
			}
			html += `</tbody></table></div>`
		}

		// add a per-table title for the Host table as well (info-icon)
		html += titleWithInfo("h3", "i18n:section.triggers_unknown_by_host", "i18n:tip.triggers")
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr>` +
			` <th data-i18n='table.host'></th>` +
			` <th data-i18n='kpi.triggers_unknown'></th>` +
			` <th data-i18n='table.errors'></th>` +
			` <th data-i18n='table.link'></th>` +
			`</tr></thead><tbody>`
		for _, row := range triggerUnknownRows {
			var trigHostLink string
			if majorV >= 7 {
				trigHostPath := fmt.Sprintf("zabbix.php?action=trigger.list&context=host&filter_hostids%%5B%%5D=%s&filter_name=&filter_state=1&filter_status=-1&filter_value=-1&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_inherited=-1&filter_discovered=-1&filter_dependent=-1&filter_set=1", row.HostID)
				trigHostLink = ambienteUrl + "/" + trigHostPath
			} else {
				trigHostLink = ambienteUrl + "/triggers.php?filter_hostids%%5B%%5D=" + row.HostID + "&filter_state=1&filter_set=1"
			}
			html += `<tr>` +
				`<td>` + htmlpkg.EscapeString(row.HostName) + `</td>` +
				`<td>` + formatInt(row.Count) + `</td>` +
				`<td>` + htmlpkg.EscapeString(row.TopErrors) + `</td>` +
				`<td><a href='` + htmlpkg.EscapeString(trigHostLink) + `' target='_blank' rel='noopener' data-i18n='open'></a></td>` +
				`</tr>`
		}
		html += `</tbody></table></div>`
	}
	html += `</div>` // end tab-triggers

	// Recomendações tab (espaço para sugestões automáticas / ações)
	html += `<div id='tab-recomendacoes' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title' data-i18n='tabs.recommendations'></h2>`
	html += titleWithInfo("h3", "i18n:section.recommendations", "i18n:tip.recommendations")

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
		if c, err := collector.CollectCount(apiUrl, token, "item.get", map[string]interface{}{
			"output": "extend",
			"templated": true,
			"countOutput": true,
			"filter": map[string]interface{}{"type": 20},
		}, zabbixApiRequest); err == nil {
			snmpTplCount = c
		}
		if c2, err2 := collector.CollectCount(apiUrl, token, "item.get", map[string]interface{}{
			"filter": map[string]interface{}{"type": 20},
			"search": map[string]interface{}{"snmp_oid": []string{"get[*", "walk[*"}},
			"searchWildcardsEnabled": true,
			"searchByAny": true,
			"countOutput": true,
			"templated": true,
		}, zabbixApiRequest); err2 == nil {
			snmpGetWalkCount = c2
		}
	}

// Busca itens SNMP em Templates que AINDA NÃO estão usando OIDs get[]/walk[], para montar a recomencaçao de migração de SNMP para versões mais modernas do Zabbix (que utilizam get[]/walk[] e não dependem do formato antigo de OID). A ideia é identificar quais templates SNMP ainda possuem itens usando o formato antigo (sem get[]/walk[]) e listar esses templates como candidatos à migração, já que eles provavelmente estão utilizando o método de coleta SNMP mais antigo e menos eficiente, para migrar para o poller assincrono.
// Coletamos seus IDs de host, resolvemos os nomes dos Templates e removemos duplicatas.
	snmpMigrationTpls := []string{} // sorted list of template names
	if majorV >= 7 {
		if respSnmpAll, errSnmpAll := collector.CollectRawList(apiUrl, token, "item.get", map[string]interface{}{
			"output":    []string{"itemid", "hostid"},
			"filter":    map[string]interface{}{"type": 20},
			"templated": true,
			"selectHosts": []string{"hostid"},
		}, zabbixApiRequest); errSnmpAll == nil {
			if respSnmpGW, errSnmpGW := collector.CollectRawList(apiUrl, token, "item.get", map[string]interface{}{
				"output":                []string{"itemid", "hostid"},
				"filter":                map[string]interface{}{"type": 20},
				"search":                map[string]interface{}{"snmp_oid": []string{"get[*", "walk[*"}},
				"searchWildcardsEnabled": true,
				"searchByAny":           true,
				"templated":             true,
				"selectHosts": []string{"hostid"},
			}, zabbixApiRequest); errSnmpGW == nil {
				// Pesquisa e constrói o conjunto de hostids que já utilizam get[]/walk[]
				modernHostids := map[string]struct{}{}
				for _, item := range respSnmpGW {
					if hosts, ok4 := item["hosts"].([]interface{}); ok4 {
						for _, h := range hosts {
							if hm, ok5 := h.(map[string]interface{}); ok5 {
								modernHostids[fmt.Sprintf("%v", hm["hostid"])] = struct{}{}
							}
						}
					}
				}
				// Coleta os hostids de TODOS os templates SNMP que possuem pelo menos um item não moderno
				legacyHostSet := map[string]struct{}{}
				for _, item := range respSnmpAll {
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
				if len(legacyHostSet) > 0 {
					legacyIds := []string{}
					for hid := range legacyHostSet { legacyIds = append(legacyIds, hid) }
					if tplArr, tplErr := collector.CollectRawList(apiUrl, token, "template.get", map[string]interface{}{
						"output":      []string{"templateid", "name"},
						"templateids": legacyIds,
						"selectHosts": []string{"hostid"}, // apenas templates vinculados a pelo menos um host
					}, zabbixApiRequest); tplErr == nil {
						for _, tm := range tplArr {
							// Descarta temtlates não utilizados por nenhum host
							hosts, _ := tm["hosts"].([]interface{})
							if len(hosts) == 0 { continue }
							snmpMigrationTpls = append(snmpMigrationTpls, fmt.Sprintf("%v", tm["name"]))
						}
						sort.Strings(snmpMigrationTpls)
					}
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
.rec-kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(145px,1fr));gap:12px;margin-bottom:20px}
.kpi{padding:14px 16px;border-radius:8px;background:#fff;box-shadow:0 2px 8px rgba(0,0,0,.06);cursor:pointer;display:flex;flex-direction:column;align-items:flex-start;transition:box-shadow .15s;border-left:4px solid transparent}
.kpi:hover{box-shadow:0 4px 14px rgba(0,0,0,.1)}
.kpi .kpi-num{font-weight:800;font-size:24px;line-height:1;margin-bottom:2px}
.kpi .kpi-label{font-size:11px;color:#5a6776;font-weight:500}
.kpi-warn{border-left-color:#ffcc00}
.kpi-crit{border-left-color:#ff6666}
.kpi-ok{border-left-color:#16a34a}
.status-badge{padding:4px 8px;border-radius:999px;font-weight:600;font-size:12px}
.status-badge.ok{background:#e6ffef;color:#065f46}
.status-badge.warn{background:#fff7e6;color:#b26b00}
.status-badge.crit{background:#fff1f0;color:#b02a2a}
details.rec-section{border:1px solid rgba(0,0,0,.09);border-radius:10px;margin-bottom:12px;overflow:hidden;background:#fff}
details.rec-section[open]{border-color:rgba(0,0,0,.18)}
details.rec-section>summary{padding:13px 16px;cursor:pointer;display:flex;align-items:center;gap:10px;list-style:none;user-select:none}
details.rec-section>summary::-webkit-details-marker{display:none}
details.rec-section>summary:hover{background:rgba(0,0,0,.02)}
.rec-sec-icon{font-size:17px;flex-shrink:0}
.rec-sec-text{flex:1;min-width:0}
.rec-sec-title{font-size:14px;font-weight:600}
.rec-sec-desc{font-size:12px;color:#64748b;margin-top:1px}
.rec-sec-arrow{color:#94a3b8;font-size:10px;transition:transform .2s;flex-shrink:0}
details.rec-section[open] .rec-sec-arrow{transform:rotate(90deg)}
.rec-sec-body{padding:0 16px 16px}
.fix-box{background:rgba(25,118,210,.04);border:1px solid rgba(25,118,210,.15);border-radius:8px;padding:12px 14px;margin-top:14px}
.fix-box-title{font-size:11px;font-weight:700;color:#1565c0;margin-bottom:8px;text-transform:uppercase;letter-spacing:.5px}
.fix-box pre{background:rgba(0,0,0,.05);border:1px solid rgba(0,0,0,.08);border-radius:5px;padding:10px;font-size:11px;overflow-x:auto;font-family:'Courier New',monospace;line-height:1.6;margin:6px 0;color:#1a2332;white-space:pre}
.fix-box code{background:rgba(0,0,0,.06);padding:1px 4px;border-radius:3px;font-family:'Courier New',monospace;font-size:11px}
.fix-box ul{padding-left:16px;margin:4px 0}
.fix-box li{font-size:12px;color:#475569;margin-bottom:3px}
</style>`


// Compute total triggers in the environment and percentage Unknown (used by KPI and Recommendations)
totalTriggersAll := 0
if nAll, errAll := collector.CollectCount(apiUrl, token, "trigger.get", nil, zabbixApiRequest); errAll == nil {
	totalTriggersAll = nAll
}
pctTriggers := 0.0
if totalTriggersAll > 0 {
	pctTriggers = (float64(totalTriggersUnknown) / float64(totalTriggersAll)) * 100.0
}
triggersPctStr := fmt.Sprintf("%.1f%%", pctTriggers)

	// SNMP-POLLER KPI (porcentagem)
	snmpPct := 0.0
	if snmpTplCount > 0 { snmpPct = (float64(snmpGetWalkCount) * 100.0) / float64(snmpTplCount) }
	html += `<div class='rec-kpis'>`
	serverAttnClass := "kpi-ok"; if attentionCount > 0 { serverAttnClass = "kpi-warn" }
	html += `<div class='kpi ` + serverAttnClass + `' data-target='#card-server' data-i18n-title='kpi.server_attention' title=''><div class='kpi-num'>` + formatInt(attentionCount) + `</div><div class='kpi-label' data-i18n='kpi.server_attention'></div></div>`
	proxyOfflineClass := "kpi-ok"; if proxyOfflineCount > 0 { proxyOfflineClass = "kpi-crit" }
	html += `<div class='kpi ` + proxyOfflineClass + `' data-target='#card-proxys' data-i18n-title='kpi.proxies_offline' title=''><div class='kpi-num'>` + formatInt(proxyOfflineCount) + `</div><div class='kpi-label' data-i18n='kpi.proxies_offline'></div></div>`
	proxyUnknownClass := "kpi-ok"; if proxyUnknownCount > 0 { proxyUnknownClass = "kpi-warn" }
	html += `<div class='kpi ` + proxyUnknownClass + `' data-target='#card-proxys' data-i18n-title='kpi.proxies_unknown' title=''><div class='kpi-num'>` + formatInt(proxyUnknownCount) + `</div><div class='kpi-label' data-i18n='kpi.proxies_unknown'></div></div>`
	// KPI: processos dos proxys com AVG alto (≥ 60%)
	proxyAttnClass := "kpi-ok"
	if len(proxyProcAttnList) > 0 { proxyAttnClass = "kpi-warn" }
	html += `<div class='kpi ` + proxyAttnClass + `' data-target='#card-proxys' data-i18n-title='kpi.proxy_process_attention' title=''><div class='kpi-num'>` + formatInt(len(proxyProcAttnList)) + `</div><div class='kpi-label' data-i18n='kpi.proxy_process_attention'></div></div>`
	// KPI Items não suportados... (verde < 4%, amarelo 4–10%, vermelho > 10%)
	unsupportedPct := 0.0
	if totalItemsVal > 0 { unsupportedPct = (float64(unsupportedCount) * 100.0) / float64(totalItemsVal) }
	itemsUnsupportedClass := "kpi-ok"
	if unsupportedPct > 10.0 { itemsUnsupportedClass = "kpi-crit" } else if unsupportedPct >= 4.0 { itemsUnsupportedClass = "kpi-warn" }
	unsupportedPctStr := fmt.Sprintf("%.1f%%", unsupportedPct)
	html += `<div class='kpi ` + itemsUnsupportedClass + `' data-target='#card-items' data-i18n-title='kpi.items_unsupported' title=''><div class='kpi-num'>` + unsupportedPctStr + `</div><div class='kpi-label' data-i18n='kpi.items_unsupported'></div></div>`
	// show SNMP KPIs only for Zabbix 7 (we computed counts earlier)
	if majorV >= 7 {
		// KPI: Templates SNMP que ainda precisam migrar para o poller assíncrono (get[]/walk[])
		migClass := "kpi-ok"
		if len(snmpMigrationTpls) > 0 { migClass = "kpi-warn" }
		html += `<div class='kpi ` + migClass + `' data-target='#card-templates' data-i18n-title='kpi.snmp_templates_migration' title=''><div class='kpi-num'>` + formatInt(len(snmpMigrationTpls)) + `</div><div class='kpi-label' data-i18n='kpi.snmp_templates_migration'></div></div>`
		// KPI: Percentual de items SNMP em templates já usando get[]/walk[]
		kclass := "kpi-crit"
		if snmpPct >= 80.0 { kclass = "kpi-ok" }
		html += `<div class='kpi ` + kclass + `' data-target='#card-items' data-i18n-title='kpi.snmp_items_label' title=''><div class='kpi-num'>` + fmt.Sprintf("%.2f%%", snmpPct) + `</div><div class='kpi-label' data-i18n='kpi.snmp_items_label'></div></div>`
	}
	textItemsClass := "kpi-ok"; if textItemsCount > 0 { textItemsClass = "kpi-warn" }
	html += `<div class='kpi ` + textItemsClass + `' data-target='#card-items' data-i18n-title='kpi.items_text_history' title=''><div class='kpi-num'>` + formatInt(textItemsCount) + `</div><div class='kpi-label' data-i18n='kpi.items_text_history'></div></div>`
	// KPI: default Admin account
	adminKpiClass := "kpi-ok"
	if hasDefaultAdmin {
		adminKpiClass = "kpi-warn"
		if adminDefaultPasswordValid { adminKpiClass = "kpi-crit" }
	}
	adminKpiIcon := "🟢"
	if hasDefaultAdmin {
		adminKpiIcon = "🟡"
		if adminDefaultPasswordValid { adminKpiIcon = "🔴" }
	}
	// KPI: Triggers Unknown (show percentage of triggers Unknown)
	triggersUnknownHostsCount := len(triggerUnknownRows)
	// Red (critical) when percent >0 and below 3%; warn when there are hosts with Unknown triggers; ok otherwise
	triggersKpiClass := "kpi-ok"
	if pctTriggers > 0.0 && pctTriggers < 3.0 {
		triggersKpiClass = "kpi-crit"
	} else if triggersUnknownHostsCount > 0 {
		triggersKpiClass = "kpi-warn"
	}
	html += `<div class='kpi ` + triggersKpiClass + `' data-target='#card-triggers' data-i18n-title='kpi.triggers_unknown' title=''>` +
		`<div class='kpi-num'>` + triggersPctStr + `</div>` +
		`<div class='kpi-label' data-i18n='kpi.triggers_unknown'></div></div>`
	html += `<div class='kpi ` + adminKpiClass + `' data-target='#card-security' data-i18n-title='kpi.default_admin' title=''>` +
		`<div class='kpi-num'>` + adminKpiIcon + `</div>` +
		`<div class='kpi-label' data-i18n='kpi.default_admin'></div></div>`
	html += `</div>`	

	html += `<script>
document.querySelectorAll('.rec-kpis .kpi').forEach(k=>k.addEventListener('click',function(){var t=this.getAttribute('data-target');if(!t)return;var el=document.querySelector(t);if(el)el.scrollIntoView({behavior:'smooth',block:'start'})}));
// ensure our info tooltips still work after possible dynamic toggles
setTimeout(setupInfoTooltips,50);
// re-fetch i18n to ensure all new keys are loaded, then re-apply
fetch('/locales/'+(_lang||'pt_BR')+'/messages.json?cb='+Date.now()).then(function(r){return r.json();}).then(function(d){_i18n=d;applyI18n();}).catch(function(){});
</script>`

	// Recomendações dinâmicas (uses precomputed aggregates above)
	// secNum auto-increments each time a top-level section is emitted;
	// subNum resets to 1 at each new section and increments per sub-item.
	secNum := 0
	nextSub := func(sub *int, label string) string {
		*sub++
		num := fmt.Sprintf("%d.%d)", secNum, *sub)
		// support i18n: keys (e.g. "i18n:sub.items_no_template") — render a small data-i18n span
		if strings.HasPrefix(label, "i18n:") {
			key := strings.TrimPrefix(label, "i18n:")
			return fmt.Sprintf("%s <span data-i18n='%s'></span>", num, key)
		}
		return num + " " + label
	}

	// Pré-computa missingAsync para decidir se a seção Zabbix Server aparece
	checkTrendEnvVal := os.Getenv("CHECKTRENDTIME")
	if checkTrendEnvVal == "" { checkTrendEnvVal = "15d" }
	asyncNames := []string{"Agent Poller", "HTTP Agent Poller", "SNMP Poller"}
	missingAsync := []string{}
	pollMap := map[string]pollRow{}
	for _, pr := range pollRows {
		pollMap[strings.ToLower(strings.TrimSpace(pr.Friendly))] = pr
	}
	for _, an := range asyncNames {
		if pr, ok := pollMap[strings.ToLower(strings.TrimSpace(an))]; ok {
			if pr.Disabled && strings.TrimSpace(pr.DisabledMsg) != "" {
				missingAsync = append(missingAsync, an)
			}
		}
	}

	// --- Seção: Zabbix Server (só aparece quando há recomendação) ---
	if len(attention) > 0 || len(missingAsync) > 0 {
		serverSub := 0
		serverParams := make([]string, 0)
		secNum++
		serverBadge := "ok"; if len(attention) > 0 { serverBadge = "warn" }
		serverBadgeIcon := "🟢"; if serverBadge == "warn" { serverBadgeIcon = "🟡" }
		serverDescParts := []string{}
		if len(attention) > 0 { serverDescParts = append(serverDescParts, fmt.Sprintf("<span data-i18n='rec.desc.server_attention' data-i18n-args='%d'></span>", len(attention))) }
		if len(missingAsync) > 0 { serverDescParts = append(serverDescParts, fmt.Sprintf("<span data-i18n='rec.desc.server_async' data-i18n-args='%d'></span>", len(missingAsync))) }
		html += `<details class='rec-section' id='card-server'>` +
			`<summary><span class='rec-sec-icon'>⚙️</span>` +
			`<div class='rec-sec-text'>` +
			`<div class='rec-sec-title'><strong>` + fmt.Sprintf("%d)", secNum) + `</strong> <span data-i18n='section.server'></span></div>` +
			`<div class='rec-sec-desc'>` + strings.Join(serverDescParts, " · ") + `</div>` +
			`</div><span class='status-badge ` + serverBadge + `'>` + serverBadgeIcon + `</span>` +
			`<span class='rec-sec-arrow'>▶</span></summary>` +
			`<div class='rec-sec-body'>`
		// single quick link to open corresponding tab
		html += `<p style='font-size:0.92em;margin-bottom:10px;'><a href='#' onclick='event.preventDefault();showTab("tab-processos");' data-i18n='rec.server_see_tab'></a></p>`
		if len(attention) > 0 {
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&serverSub, "i18n:sub.server_suggestions"))
			html += `<p><span data-i18n='sub.customize_processes_threads'></span></p>`
			html += `<ol style='margin-left:18px;font-size:0.88em;'>`
			serverParams = make([]string, 0)
			for _, a := range attention {
				param := procToParam(a.Name)
				html += `<li>` + htmlpkg.EscapeString(a.Name) + ` — avg: ` + fmt.Sprintf("%.2f%%", a.Vavg) + `</li>`
				if param != "" {
					// support multiple params separated by comma (e.g. Housekeeper -> HousekeepingFrequency,MaxHousekeeperDelete)
					for _, pp := range strings.Split(param, ",") {
						pp = strings.TrimSpace(pp)
						if pp == "" { continue }
						found := false
						for _, ex := range serverParams { if ex == pp { found = true; break } }
						if !found { serverParams = append(serverParams, pp) }
					}
				}
			}
			html += `</ol>`
		}
		if len(missingAsync) > 0 {
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&serverSub, "i18n:sub.use_async_pollers"))
			html += `<p data-i18n='tip.pollers' data-i18n-args='` + htmlpkg.EscapeString(checkTrendDisplay) + `'></p>`
			html += `<div style='margin-left:6px;font-size:0.88em;'><ul>`
			for _, n := range missingAsync {
				key := strings.ToLower(strings.TrimSpace(n))
				tipKey := ""
				if v, ok := procDesc[key]; ok { tipKey = "i18n:" + v }
				html += `<li>` + titleWithInfo("span", n, tipKey) + `</li>`
			}
			html += `</ul></div>`
		}
		// Dynamic per-process fixes: reuse procDesc and titleWithInfo
		html += `<div class='fix-box'><div class='fix-box-title'>🔧 <span data-i18n='fix.how_to_resolve'></span></div>`
		if len(attention) > 0 {
			html += `<ul>`
			for _, a := range attention {
				lname := strings.ToLower(strings.TrimSpace(a.Name))
				tipKey := "procdesc.process"
				if v, ok := procDesc[lname]; ok { tipKey = v }
				param := procToParam(a.Name)
				html += `<li>` + titleWithInfo("span", a.Name, "i18n:"+tipKey)
				if param != "" {
					html += ` — <code>` + param + `</code> <span style='color:#6b7280;font-size:0.9em;margin-left:6px;' data-i18n='fix.server_intro'></span>`
				}
				html += `</li>`
			}
			html += `</ul>`
			// restart hint (single global snippet)
			if len(serverParams) > 0 {
				html += "<pre># /etc/zabbix/zabbix_server.conf\n"
				for _, p := range serverParams {
					html += p + "=   # <span data-i18n='fix.proxy_increase_hint'></span>\n"
				}
				html += "systemctl restart zabbix-server</pre>"
				// If housekeeper params are present, suggest DB partitioning / TimescaleDB
				hasHousekeeper := false
				for _, p := range serverParams {
					if strings.EqualFold(p, "HousekeepingFrequency") || strings.EqualFold(p, "MaxHousekeeperDelete") {
						hasHousekeeper = true
						break
					}
				}
				if hasHousekeeper {
					html += `<div style='margin-top:8px;font-size:0.9em;color:#374151;'><span data-i18n='fix.db_maintenance_advice'></span></div>`
				}
			} else {
				html += "<pre style='margin-top:8px;'>systemctl restart zabbix-server</pre>"
			}
		} else {
			html += `<p style='margin:0 0 6px;'><span data-i18n='fix.server_intro'></span></p>`
			html += "<pre># /etc/zabbix/zabbix_server.conf\n"
			if majorV >= 7 {
				html += "# Zabbix 7 — async pollers (recommended)\nStartAgentPollers=\nStartHTTPAgentPollers=\nStartSNMPPollers=\n"
			}
			html += "systemctl restart zabbix-server</pre>"
		}
		html += `</div>` // fix-box
		html += `</div></details>` // rec-sec-body + accordion
	}

	// --- Seção: Zabbix Proxys (Unknown, Offline ou processos em Atenção ou sem template) ---
	if unknown > 0 || offline > 0 || len(proxyProcAttnList) > 0 || len(proxyNoTemplateList) > 0 || len(proxyMissingAsyncMap) > 0 {
		proxySub := 0
		secNum++
		proxyBadge := "ok"
		if offline > 0 { proxyBadge = "crit" } else if unknown > 0 || len(proxyProcAttnList) > 0 { proxyBadge = "warn" }
		proxyBadgeIcon := "🟢"
		if proxyBadge == "crit" { proxyBadgeIcon = "🔴" } else if proxyBadge == "warn" { proxyBadgeIcon = "🟡" }
		proxyDescParts := []string{}
		if offline > 0 { proxyDescParts = append(proxyDescParts, fmt.Sprintf("<span data-i18n='rec.desc.proxy_offline' data-i18n-args='%d'></span>", offline)) }
		if unknown > 0 { proxyDescParts = append(proxyDescParts, fmt.Sprintf("<span data-i18n='rec.desc.proxy_unknown' data-i18n-args='%d'></span>", unknown)) }
		if len(proxyProcAttnList) > 0 { proxyDescParts = append(proxyDescParts, fmt.Sprintf("<span data-i18n='rec.desc.proxy_process_attn' data-i18n-args='%d'></span>", len(proxyProcAttnList))) }
		html += `<details class='rec-section' id='card-proxys'>` +
			`<summary><span class='rec-sec-icon'>📡</span>` +
			`<div class='rec-sec-text'>` +
			`<div class='rec-sec-title'><strong>` + fmt.Sprintf("%d)", secNum) + `</strong> <span data-i18n='section.proxies'></span></div>` +
			`<div class='rec-sec-desc'>` + strings.Join(proxyDescParts, " · ") + `</div>` +
			`</div><span class='status-badge ` + proxyBadge + `'>` + proxyBadgeIcon + `</span>` +
			`<span class='rec-sec-arrow'>▶</span></summary>` +
			`<div class='rec-sec-body'>`
		// single quick link to open corresponding tab
		html += `<p style='font-size:0.92em;margin-bottom:10px;'><a href='#' onclick='event.preventDefault();showTab("tab-proxys");' data-i18n='rec.proxys_see_tab'></a></p>`
			if len(proxyProcAttnList) > 0 {
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&proxySub, "i18n:sub.customize_processes_threads"))
			html += `<p data-i18n='tip.proxy_processes' data-i18n-args='` + htmlpkg.EscapeString(checkTrendDisplay) + `'></p>`
			html += `<ol style='margin-left:18px;font-size:0.88em;'>`
			for _, a := range proxyProcAttnList {
				html += `<li>` + htmlpkg.EscapeString(a.ProxyName) + ` — ` + htmlpkg.EscapeString(a.ProcFriendly) + ` — avg: ` + fmt.Sprintf("%.2f%%", a.Vavg) + `</li>`
			}
			html += `</ol>`
		}
			if len(proxyMissingAsyncMap) > 0 {
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&proxySub, "i18n:sub.use_async_pollers"))
			html += `<p data-i18n='tip.proxy_processes' data-i18n-args='` + htmlpkg.EscapeString(checkTrendDisplay) + `'></p>`
			// Ordena os proxies para exibição determinística
			sortedProxyNames := make([]string, 0, len(proxyMissingAsyncMap))
			for pn := range proxyMissingAsyncMap { sortedProxyNames = append(sortedProxyNames, pn) }
			sort.Strings(sortedProxyNames)
			html += `<ul style='margin-left:6px;font-size:0.88em;'>`
			for _, pn := range sortedProxyNames {
				html += `<li>` + htmlpkg.EscapeString(pn) + `—`
				for _, pollerName := range proxyMissingAsyncMap[pn] {
					key := strings.ToLower(strings.TrimSpace(pollerName))
					tipKey := ""
					if v, ok := procDesc[key]; ok { tipKey = "i18n:" + v }
					if tipKey == "" { tipKey = pollerName }
					html += ` ` + titleWithInfo("span", pollerName, tipKey) + ` —`
				}
				// removemos trailing " —"
				html = html[:len(html)-len(" —")]
				html += `</li>`
			}
			html += `</ul>`
		}
		if unknown > 0 {
			tipUnknown := "i18n:tip.proxy_unknown|" + htmlpkg.EscapeString(ambienteUrl)
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&proxySub, "i18n:sub.proxies_status_unknown"))
			html += `<p style='font-size:0.88em;'><span data-i18n='proxy.detected_with_status' data-i18n-args='` + formatInt(unknown) + `'></span> ` + titleWithInfo("span", "i18n:proxy.status_unknown", tipUnknown) + `</p>`
			html += `<ul style='font-size:0.88em;'>`
			for _, n := range unknownNames { html += `<li>` + htmlpkg.EscapeString(n) + `</li>` }
			html += `</ul>`
		}
		if offline > 0 {
			tipOffline := "i18n:tip.proxy_offline|" + htmlpkg.EscapeString(ambienteUrl)
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&proxySub, "i18n:sub.proxies_offline"))
			html += `<p style='font-size:0.88em;'><span data-i18n='proxy.detected_with_status' data-i18n-args='` + formatInt(offline) + `'></span> ` + titleWithInfo("span", "i18n:proxy.status_offline", tipOffline) + `</p>`
			html += `<ul style='font-size:0.88em;'>`
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
			html += fmt.Sprintf("<h5>%s</h5>", nextSub(&proxySub, "i18n:sub.proxy_without_monitoring"))
			html += `<p data-i18n='tip.proxy_no_template'></p>`
			html += `<p style='font-size:0.88em;'><span data-i18n='rec.proxy_no_template_prefix'></span> ` + tplSearchLink + ` <span data-i18n='rec.proxy_no_template_suffix'></span></p>`
			html += `<ol style='margin-left:18px;font-size:0.88em;'>`
			for _, pt := range proxyNoTemplateList {
				// Zabbix 7: filter_host (campo display name); Zabbix 6: filter_host (campo technical name)
				// Ambos recebem &filter_set=1 para aplicar o filtro automaticamente
				hostLinkPath := "/zabbix.php?action=host.list&filter_host=" + htmlpkg.EscapeString(pt.ProxyName) + "&filter_dns=&filter_ip=&filter_port=&filter_status=-1&filter_monitored_by=-1&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_set=1"
				if majorV < 7 {
					hostLinkPath = "/zabbix.php?action=host.list&filter_host=" + htmlpkg.EscapeString(pt.ProxyName) + "&filter_set=1"
				}
				hostLink := `<a href='` + htmlpkg.EscapeString(ambienteUrl+hostLinkPath) + `' target='_blank' rel='noopener'>` + htmlpkg.EscapeString(pt.ProxyName) + `</a>`
				html += `<li>` + hostLink + `</li>`
			}
			html += `</ol>`
		}
		html += `<div class='fix-box'><div class='fix-box-title'>🔧 <span data-i18n='fix.how_to_resolve'></span></div>`
		// Per-proxy process fixes when there are attn rows
		if len(proxyProcAttnList) > 0 {
			// Pass 1: pre-build all suggested params per proxy (deduped, order preserved)
			proxyParams := map[string][]string{}
			// also collect the process friendly names per proxy for display in the fix header
			proxyProcNames := map[string][]string{}
			proxyOrder := []string{}
			procParamMap := map[string]string{} // p.ProxyName+"|"+p.ProcFriendly -> param
			for _, p := range proxyProcAttnList {
				param := procToParam(p.ProcFriendly)
				procParamMap[p.ProxyName+"|"+p.ProcFriendly] = param
				// collect process friendly names (dedupe)
				if _, seen := proxyProcNames[p.ProxyName]; !seen {
					proxyProcNames[p.ProxyName] = []string{}
				}
				foundProc := false
				for _, ex := range proxyProcNames[p.ProxyName] { if ex == p.ProcFriendly { foundProc = true; break } }
				if !foundProc { proxyProcNames[p.ProxyName] = append(proxyProcNames[p.ProxyName], p.ProcFriendly) }
				if _, seen := proxyParams[p.ProxyName]; !seen {
					proxyOrder = append(proxyOrder, p.ProxyName)
					proxyParams[p.ProxyName] = []string{}
				}
				if param != "" {
					dup := false
					for _, ex := range proxyParams[p.ProxyName] { if ex == param { dup = true; break } }
					if !dup { proxyParams[p.ProxyName] = append(proxyParams[p.ProxyName], param) }
				}
			}
			_ = proxyOrder

			// Pass 2: render <ul> with inline <pre> after the first <li> of each proxy
			html += `<ul>`
			emittedCodeBox := map[string]bool{}
			for _, p := range proxyProcAttnList {
				lname := strings.ToLower(strings.TrimSpace(p.ProcFriendly))
				tipKey := "procdesc.process"
				if v, ok := procDesc[lname]; ok { tipKey = v }
				param := procParamMap[p.ProxyName+"|"+p.ProcFriendly]
				html += `<li>` + titleWithInfo("span", p.ProxyName+" — "+p.ProcFriendly, "i18n:"+tipKey)
				if param != "" {
					html += ` — <code>` + param + `</code> — <code>/etc/zabbix/zabbix_proxy.conf</code>`
				}
				html += `</li>`
				// Emit the code box once, right after the first <li> for this proxy
				if !emittedCodeBox[p.ProxyName] {
					emittedCodeBox[p.ProxyName] = true
					if params := proxyParams[p.ProxyName]; len(params) > 0 {
						// Show proxy and process name(s) header so user sees which host the snippet targets
						if names, ok := proxyProcNames[p.ProxyName]; ok && len(names) > 0 {
							// header intentionally suppressed here (rendered earlier in the list)
							_ = names
						}
						html += "<pre># /etc/zabbix/zabbix_proxy.conf\n"
						for _, pr := range params {
							html += pr + "=   # <span data-i18n='fix.proxy_increase_hint'></span>\n"
						}
						html += "systemctl restart zabbix-proxy</pre>"
					}
				}
			}
			html += `</ul>`
		}
		// Renderizar fix-box para forcar o fixbox styling mesmo quando só há missing async pollers ou proxies offline/unknown (sem processos em atenção)
		html += `<ul>`
		if len(proxyMissingAsyncMap) > 0 {
			html += `<li><span data-i18n='fix.proxy_highlight_async_title'></span>` +
				"<pre># /etc/zabbix/zabbix_proxy.conf\n# Zabbix 7 — async pollers (recommended)\nStartAgentPollers= \nStartHTTPPollers= \nStartSNMPPollers= \nsystemctl restart zabbix-proxy</pre></li>"
		}
		if offline > 0 || unknown > 0 {
			html += `<li><span data-i18n='fix.proxy_highlight_offline_title'></span>` +
				"<pre>systemctl status zabbix-proxy\ntail -100 /var/log/zabbix/zabbix_proxy.log\nnc -zv &lt;server&gt; 10051</pre></li>"
		}
		if len(proxyNoTemplateList) > 0 {
			html += `<li><span data-i18n='fix.proxy_no_template_hint'></span>` +
				`<div style='margin-top:4px;'><span data-i18n='fix.proxy_no_template_action'></span></div></li>`
		}
		// Compatibility recommendation: show if any proxy reports outdated or unsupported compatibility
		if len(proxyCompatOutdated) > 0 || len(proxyCompatUnsupported) > 0 {
			docURL := "https://www.zabbix.com/documentation/7.0/en/manual/installation/upgrade"
			docLink := `<a href='` + htmlpkg.EscapeString(docURL) + `' target='_blank' rel='noopener'>` + htmlpkg.EscapeString(docURL) + `</a>`
			html += `<li><span data-i18n='fix.proxy_compatibility_title'></span>` +
				`<div style='margin-top:4px;font-size:0.95em;'><span data-i18n='fix.proxy_compatibility_action'></span> ` + docLink + `</div>`
			if len(proxyCompatOutdated) > 0 {
				esc := make([]string, 0, len(proxyCompatOutdated))
				for _, n := range proxyCompatOutdated { esc = append(esc, htmlpkg.EscapeString(n)) }
				html += `<div style='margin-top:4px;font-size:0.88em;'>Outdated: ` + strings.Join(esc, ", ") + `</div>`
			}
			if len(proxyCompatUnsupported) > 0 {
				esc2 := make([]string, 0, len(proxyCompatUnsupported))
				for _, n := range proxyCompatUnsupported { esc2 = append(esc2, htmlpkg.EscapeString(n)) }
				html += `<div style='margin-top:4px;font-size:0.88em;'>Unsupported: ` + strings.Join(esc2, ", ") + `</div>`
			}
			html += `</li>`
		}
		html += `</ul>`
		html += `</div>`
		html += `</div></details>` // rec-sec-body + accordion
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
	// --- Seção: Items (só aparece quando há subitem com dado) ---
	itemsHasData := itemsNoTplCount > 0 || unsupportedVal > 0 || disabledCount > 0 || itemsLe60 > 0 || textCount > 0 || (majorV >= 7 && snmpTplCount > 0)
	if itemsHasData {
		itemsSub := 0
		secNum++
		itemsBadge := "ok"
		if unsupportedVal > 0 { itemsBadge = "crit" } else if itemsLe60 > 0 || textCount > 0 || itemsNoTplCount > 0 { itemsBadge = "warn" }
		itemsBadgeIcon := "🟢"
		if itemsBadge == "crit" { itemsBadgeIcon = "🔴" } else if itemsBadge == "warn" { itemsBadgeIcon = "🟡" }
		itemsDescParts := []string{}
		if unsupportedVal > 0 { itemsDescParts = append(itemsDescParts, "<span data-i18n='rec.desc.items_unsupported' data-i18n-args='"+formatInt(unsupportedVal)+"'></span>") }
		if itemsLe60 > 0 { itemsDescParts = append(itemsDescParts, "<span data-i18n='rec.desc.items_short_interval' data-i18n-args='"+formatInt(itemsLe60)+"'></span>") }
		if textCount > 0 { itemsDescParts = append(itemsDescParts, "<span data-i18n='rec.desc.items_text_history' data-i18n-args='"+formatInt(textCount)+"'></span>") }
		html += `<details class='rec-section' id='card-items'>` +
			`<summary><span class='rec-sec-icon'>📋</span>` +
			`<div class='rec-sec-text'>` +
			`<div class='rec-sec-title'><strong>` + fmt.Sprintf("%d)", secNum) + `</strong> <span data-i18n='section.items'></span></div>` +
			`<div class='rec-sec-desc'>` + strings.Join(itemsDescParts, " · ") + `</div>` +
			`</div><span class='status-badge ` + itemsBadge + `'>` + itemsBadgeIcon + `</span>` +
			`<span class='rec-sec-arrow'>▶</span></summary>` +
			`<div class='rec-sec-body'>`
		// single quick link to open corresponding tab (Items)
		html += `<p style='font-size:0.92em;margin-bottom:10px;'><a href='#' onclick='event.preventDefault();showTab("tab-items");' data-i18n='rec.items_see_tab'></a></p>`
		html += `<div style='margin-left:6px;font-size:0.88em;'>`
			if itemsNoTplCount > 0 {
				html += `<p><strong>` + nextSub(&itemsSub, "i18n:sub.items_no_template") + `</strong> <span data-i18n='items.no_template_paragraph' data-i18n-args='` + formatInt(itemsNoTplCount) + `'></span> <a href='` + itemsNoTplLink + `' target='_blank' rel='noopener' data-i18n='open_full_listing'></a></p>`
			}
			if unsupportedVal > 0 {
				html += `<p><strong>` + nextSub(&itemsSub, "i18n:sub.items_unsupported") + `</strong> <span data-i18n='items.unsupported_paragraph' data-i18n-args='` + formatInt(unsupportedVal) + `|` + pct(unsupportedVal, totalItemsVal) + `'></span> <a href='` + unsupportedLink + `' target='_blank' rel='noopener' data-i18n='open_full_listing'></a></p>`
			}
			if disabledCount > 0 {
				html += `<p><strong>` + nextSub(&itemsSub, "i18n:sub.items_disabled") + `</strong> <span data-i18n='items.disabled_paragraph' data-i18n-args='` + formatInt(disabledCount) + `|` + pct(disabledCount, totalItemsVal) + `'></span> <a href='` + itemsDisabledLink + `' target='_blank' rel='noopener' data-i18n='open_full_listing'></a></p>`
			}
			if itemsLe60 > 0 {
				html += `<p><strong>` + nextSub(&itemsSub, "i18n:sub.items_interval_le_60") + `</strong> <span data-i18n='items.interval_le_60_paragraph' data-i18n-args='` + formatInt(itemsLe60) + `'></span></p>`
			}
			if textCount > 0 {
				// Build version-aware link to open the full listing in Zabbix frontend
				var textItemsFullLink string
				if majorV >= 7 {
					textItemsFullLink = ambienteUrl + "/zabbix.php?action=item.list&context=host&filter_name=&filter_type=-1&filter_key=&filter_snmp_oid=&filter_value_type=4&filter_delay=300&filter_history=1w&filter_trends=&filter_status=-1&filter_state=-1&filter_inherited=-1&filter_with_triggers=-1&filter_discovered=-1&filter_evaltype=0&filter_profile=web.hosts.items.list.filter&filter_tab=1&sort=name&sortorder=ASC"
				} else {
					// Legacy frontend path for Zabbix < 7 — keep filters similar to modern listing
					textItemsFullLink = ambienteUrl + "/items.php?filter_value_type=4&filter_delay=300&filter_history=1w&filter_status=-1&sort=name&sortorder=ASC"
				}
				html += `<p><strong>` + nextSub(&itemsSub, "i18n:sub.items_text_with_history") + `</strong> <span data-i18n='items.text_with_history_paragraph' data-i18n-args='` + formatInt(textCount) + `'></span> <a href='` + htmlpkg.EscapeString(textItemsFullLink) + `' target='_blank' rel='noopener' data-i18n='open_full_listing'></a></p>`
			}
		if majorV >= 7 && snmpTplCount > 0 {
			snmpIcon := `<span class='info-icon' tabindex='0' style='display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;cursor:pointer;margin-left:4px;position:relative;vertical-align:middle;'>` +
				`<svg viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='1.6' fill='white'/><text x='8' y='11' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
				`<span class='info-tooltip' data-i18n='tip.snmp_migration'></span></span>`
					html += `<p><strong>` + nextSub(&itemsSub, "i18n:sub.items_snmp_poller") + `</strong>` + snmpIcon + ` <span data-i18n='items.snmp_poller_paragraph' data-i18n-args='` + formatInt(snmpTplCount) + `|` + formatInt(snmpGetWalkCount) + `|` + pct(snmpGetWalkCount, totalItemsVal) + `'></span></p>`
		}
		html += `</div>`
		// items fix-box: only show bullets for counts > 0
		html += `<div class='fix-box'><div class='fix-box-title'>🔧 <span data-i18n='fix.how_to_resolve'></span></div>`
		html += `<ul>`
		if itemsNoTplCount > 0 {
			html += `<li><span data-i18n='fix.items_no_template_hint'></span></li>`
		}
		if disabledCount > 0 {
			html += `<li><span data-i18n='fix.items_disabled_hint'></span></li>`
		}
		if unsupportedVal > 0 {
			html += `<li><span data-i18n='fix.items_unsupported_hint'></span></li>`
		}
		if itemsLe60 > 0 {
			html += `<li><span data-i18n='fix.items_short_interval_hint'></span>: <code>delay ≥ 60s</code></li>`
		}
		if textCount > 0 {
			html += `<li><span data-i18n='fix.items_text_history_hint'></span>: <code>History = Do not store</code></li>`
		}
		html += `</ul></div>`
		html += `</div></details>` // rec-sec-body + accordion
	}

	// --- Seção: Triggers Unknown (só aparece quando há triggers em unknown) ---
	if len(triggerUnknownRows) > 0 {
		secNum++
		html += `<details class='rec-section' id='card-triggers'>` +
			`<summary><span class='rec-sec-icon'>⚠️</span>` +
			`<div class='rec-sec-text'>` +
			`<div class='rec-sec-title'><strong>` + fmt.Sprintf("%d)", secNum) + `</strong> <span data-i18n='rec.triggers_review'></span></div>` +
			`<div class='rec-sec-desc'><span data-i18n='rec.desc.triggers_unknown' data-i18n-args='` + fmt.Sprintf("%d", len(triggerUnknownRows)) + `'></span></div>` +
			`</div><span class='status-badge crit'>🔴</span>` +
			`<span class='rec-sec-arrow'>▶</span></summary>` +
			`<div class='rec-sec-body'>`
		trigAllLink := ""

		// totalTriggersAll and pctTriggers were computed earlier for the KPI; reuse them here
		templatesAffected := templatesShownCount
		if majorV >= 7 {
			trigAllLink = ambienteUrl + "/zabbix.php?action=trigger.list&context=host&filter_name=&filter_state=1&filter_status=-1&filter_value=-1&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_inherited=-1&filter_discovered=-1&filter_dependent=-1&filter_set=1"
		} else {
			trigAllLink = ambienteUrl + "/triggers.php?filter_state=1&filter_set=1"
		}
		// use i18n key with args: totalUnknown|pct_str|templates|hosts
		args := fmt.Sprintf("%d|%s|%d|%d", totalTriggersUnknown, triggersPctStr, templatesAffected, len(triggerUnknownRows))
		html += `<p style='font-size:0.92em;margin-bottom:10px;'><span data-i18n='rec.triggers_summary_env' data-i18n-args='` + args + `'></span> ` +
			`<a href='#' onclick='event.preventDefault();showTab("tab-triggers");' data-i18n='rec.triggers_see_tab'></a>. ` +
			`<a href='`+htmlpkg.EscapeString(trigAllLink)+`' target='_blank' rel='noopener' data-i18n='open_full_listing'></a>` +
			`</p>`
		html += `<div class='fix-box'><div class='fix-box-title'>🔧 <span data-i18n='fix.how_to_resolve'></span></div>` +
			`<ul>` +
			`<li><span data-i18n='fix.triggers_unknown_hint'></span></li>` +
			`<li><span data-i18n='fix.triggers_items_hint'></span></li>` +
			`</ul></div>`
		html += `</div></details>`
	}

	// --- Seção: Regras de LLD (só aparece quando há dado) ---
	if lldLe300 > 0 || lldNotSupCnt > 0 {
		lldSub := 0
		secNum++
		lldBadge := "warn"
		lldBadgeIcon := "🟡"
		lldDescParts := []string{}
		if lldLe300 > 0 { lldDescParts = append(lldDescParts, fmt.Sprintf("<span data-i18n='rec.desc.lld_short_interval' data-i18n-args='%d'></span>", lldLe300)) }
		if lldNotSupCnt > 0 { lldDescParts = append(lldDescParts, fmt.Sprintf("<span data-i18n='rec.desc.lld_unsupported' data-i18n-args='%d'></span>", lldNotSupCnt)) }
		html += `<details class='rec-section' id='card-lld'>` +
			`<summary><span class='rec-sec-icon'>🔍</span>` +
			`<div class='rec-sec-text'>` +
			`<div class='rec-sec-title'><strong>` + fmt.Sprintf("%d)", secNum) + `</strong> <span data-i18n='section.lld'></span></div>` +
			`<div class='rec-sec-desc'>` + strings.Join(lldDescParts, " · ") + `</div>` +
			`</div><span class='status-badge ` + lldBadge + `'>` + lldBadgeIcon + `</span>` +
			`<span class='rec-sec-arrow'>▶</span></summary>` +
			`<div class='rec-sec-body'>`
		// single quick link to open corresponding tab (LLD uses Items tab)
		html += `<p style='font-size:0.92em;margin-bottom:10px;'><a href='#' onclick='event.preventDefault();showTab("tab-items");' data-i18n='rec.lld_see_tab'></a></p>`
		html += `<div style='margin-left:6px;font-size:0.88em;'>`
		if lldLe300 > 0 {
				html += `<p><strong>` + nextSub(&lldSub, "i18n:sub.lld_interval_le_300") + `</strong> <span data-i18n='lld.interval_le_300_paragraph' data-i18n-args='` + formatInt(lldLe300) + `'></span></p>`
		}
		if lldNotSupCnt > 0 {
				html += `<p><strong>` + nextSub(&lldSub, "i18n:sub.lld_not_supported") + `</strong> <span data-i18n='lld.not_supported_paragraph' data-i18n-args='` + formatInt(lldNotSupCnt) + `'></span></p>`
		}
		html += `</div>`
		// LLD fix-box: show only relevant hints
		html += `<div class='fix-box'><div class='fix-box-title'>🔧 <span data-i18n='fix.how_to_resolve'></span></div>`
		html += `<ul>`
		if lldNotSupCnt > 0 {
			html += `<li><span data-i18n='fix.lld_unsupported_hint'></span>: <code>zabbix_get -s &lt;host_ip&gt; -p 10050 -k &lt;lld_key&gt;</code></li>`
			html += `<li><span data-i18n='fix.lld_json_format'></span>: <code>{"data":[{...}]}</code></li>`
		}
		if lldLe300 > 0 {
			html += `<li><span data-i18n='fix.lld_short_interval_hint'></span></li>`
		}
		html += `</ul></div>`
		html += `</div></details>` // rec-sec-body + accordion
	}

	// --- Seção: Templates (só aparece quando há dado) ---
	tplHasData := len(topTemplates) > 0 || len(topErrors) > 0 || (majorV >= 7 && len(snmpMigrationTpls) > 0)
	if tplHasData {
		tplSub := 0
		secNum++
		tplBadgeIcon := "🟡"
		tplDescParts := []string{}
		if len(topTemplates) > 0 { tplDescParts = append(tplDescParts, fmt.Sprintf("<span data-i18n='rec.desc.templates_review' data-i18n-args='%d'></span>", templatesShown)) }
		if len(topErrors) > 0 { tplDescParts = append(tplDescParts, fmt.Sprintf("<span data-i18n='rec.desc.templates_error_types' data-i18n-args='%d'></span>", func() int { if len(topErrors) > topN { return topN }; return len(topErrors) }())) }
		if majorV >= 7 && len(snmpMigrationTpls) > 0 { tplDescParts = append(tplDescParts, fmt.Sprintf("<span data-i18n='rec.desc.templates_snmp_migrate' data-i18n-args='%d'></span>", len(snmpMigrationTpls))) }
		html += `<details class='rec-section' id='card-templates'>` +
			`<summary><span class='rec-sec-icon'>📑</span>` +
			`<div class='rec-sec-text'>` +
			`<div class='rec-sec-title'><strong>` + fmt.Sprintf("%d)", secNum) + `</strong> <span data-i18n='section.templates'></span></div>` +
			`<div class='rec-sec-desc'>` + strings.Join(tplDescParts, " · ") + `</div>` +
			`</div><span class='status-badge warn'>` + tplBadgeIcon + `</span>` +
			`<span class='rec-sec-arrow'>▶</span></summary>` +
			`<div class='rec-sec-body'>`
		// single quick link to open corresponding tab (Templates)
		html += `<p style='font-size:0.92em;margin-bottom:10px;'><a href='#' onclick='event.preventDefault();showTab("tab-templates");' data-i18n='rec.templates_see_tab'></a></p>`
		html += `<p data-i18n='tip.templates_more' style='font-size:0.88em;margin:0 0 8px 0;'></p>`
		html += `<div style='margin-left:6px;font-size:0.88em;'>`
		if len(topTemplates) > 0 {
			html += fmt.Sprintf("<h5>%d.%d) <span data-i18n='rec.templates_for_review'></span></h5>", secNum, func() int { tplSub++; return tplSub }())
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
		if len(topErrors) > 0 {
			html += fmt.Sprintf("<h5>%d.%d) <span data-i18n='rec.common_errors_details'></span></h5>", secNum, func() int { tplSub++; return tplSub }())
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
			html += fmt.Sprintf("<h5>%d.%d) <span data-i18n='sub.templates_snmp_migration'></span></h5>", secNum, func() int { tplSub++; return tplSub }())
			html += `<p data-i18n='tip.snmp_migration'></p>`
			html += `<ul>`
			for _, name := range snmpMigrationTpls {
				html += `<li>` + htmlpkg.EscapeString(name) + `</li>`
			}
			html += `</ul>`
		}
		html += `</div>`
		html += `<div class='fix-box'><div class='fix-box-title'>🔧 <span data-i18n='fix.how_to_resolve'></span></div>` +
			`<ul>` +
			`<li><span data-i18n='fix.templates_tab_hint'></span></li>` +
			`<li><span data-i18n='fix.templates_update_hint'></span></li>`
		if majorV >= 7 && len(snmpMigrationTpls) > 0 {
			html += `<li><span data-i18n='fix.templates_snmp_hint'></span>: <code>get[OID]</code> / <code>walk[OID]</code></li>`
		}
		html += `</ul></div>`
		html += `</div></details>` // rec-sec-body + accordion
	}

	// --- Seção: Segurança (conta Admin padrão) ---
	if hasDefaultAdmin {
		secNum++
		securitySub := 0
		// badge: warn by default, escalate to crit when default password is accepted
		badgeClass := "warn"
		badgeIcon := "🟡"
		if adminDefaultPasswordValid { badgeClass = "crit"; badgeIcon = "🔴" }

		html += `<details class='rec-section' id='card-security'>` +
			`<summary><span class='rec-sec-icon'>🔐</span>` +
			`<div class='rec-sec-text'>` +
			`<div class='rec-sec-title'><strong>` + fmt.Sprintf("%d)", secNum) + `</strong> <span data-i18n='section.security'></span></div>` +
			`<div class='rec-sec-desc'><span data-i18n='rec.desc.default_admin'></span>` +
			func() string {
				if adminDefaultPasswordValid {
					return ` <br/><span style='color:#b91c1c;font-weight:700;' data-i18n='fix.default_admin_password_in_use'></span>`
				}
				return ""
			}() +
			`</div>` +
			`</div><span class='status-badge ` + badgeClass + `'>` + badgeIcon + `</span>` +
			`<span class='rec-sec-arrow'>▶</span></summary>` +
			`<div class='rec-sec-body'>`
		// single quick link to open the Users tab
		html += `<p style='font-size:0.92em;margin-bottom:10px;'><a href='#' onclick='event.preventDefault();showTab("tab-usuarios");' data-i18n='rec.users_see_tab'></a></p>` +
			`<h5>` + nextSub(&securitySub, "i18n:sub.default_admin_account") + ` <span data-i18n='sub.default_admin_account'></span></h5>` +
			`<p data-i18n='tip.default_admin'></p>` +
			`<div class='fix-box'><div class='fix-box-title'>🔧 <span data-i18n='fix.how_to_resolve'></span></div>` +
			`<ul>` +
			`<li><span data-i18n='fix.default_admin_change_password'></span></li>` +
			`<li><span data-i18n='fix.default_admin_rename'></span></li>` +
			`<li><span data-i18n='fix.default_admin_disable'></span></li>` +
			`</ul>` +
			func() string {
				if adminDefaultPasswordValid {
					return "<ul><li><span data-i18n='fix.default_admin_password_in_use'></span></li></ul>"
				}
				return ""
			}() +
			`<a href='` + htmlpkg.EscapeString(ambienteUrl+"/zabbix.php?action=user.list&filter_username=Admin&filter_name=&filter_surname=&filter_set=1") + `' target='_blank' rel='noopener' style='display:inline-block;margin-top:6px;font-size:0.85em;' data-i18n='fix.open_users_list'></a>` +
			`</div>` +
			`</div></details>`
	}

	html += `</div>` // fecha tab-recomendacoes \O/

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
	r.Static("/locales", "./web/locales")
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
		setTask(id, &Task{ID: id, Status: "processing", ProgressMsg: "progress.starting_collection"})
		go func(taskID string, url, token string) {
			setProgress := func(msg string) {
				if t := getTask(taskID); t != nil { t.ProgressMsg = msg }
			}
			setProgress("progress.detecting_version")
			report, err := generateZabbixReportWithProgress(url, token, setProgress)
			if err != nil {
				log.Printf("[ERROR] Erro na tarefa %s: %v", taskID, err)
				if t := getTask(taskID); t != nil {
					tasksMu.Lock()
					t.Status = "error"
					if strings.Contains(err.Error(), "Not authorized") || strings.Contains(err.Error(), "Not authorised") {
						t.Report = "<div style='color:red;'><span data-i18n='error.invalid_token'></span></div>"
					} else {
						t.Report = "<div style='color:red;'><span data-i18n='error_server'></span> " + htmlpkg.EscapeString(err.Error()) + "</div>"
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
				t.ProgressMsg = "progress.report_generated" // final
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
				// Aplica ma parte documento completo com CSS / JS / inicialização de gauge
				cssLink := `<link rel="stylesheet" href="/static/style.css">`
				jsChart := `<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>`
				extra := `<script src="/static/script.js"></script>`
					initInline := `<script>window.addEventListener('load', function(){ try{ if (typeof initGauges === 'function') initGauges(document.body); }catch(e){console&&console.error(e);} });</script>`
					full := "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Relatório Zabbix - " + htmlpkg.EscapeString(name) + "</title>" + cssLink + jsChart + `</head><body>` + s + extra + initInline + `</body></html>`
					c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(full))
				})

				// Deletar um único relatório por id
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

				// Deletar todos os relatórios
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

// Wrapper para gerar progresso do relatorio
			func generateZabbixReportWithProgress(url, token string, setProgress func(string)) (string, error) {
				if setProgress != nil { setProgress("progress.detecting_version") }
				return generateZabbixReport(url, token, setProgress)
			}

//Se chegou até aqui, parabens!