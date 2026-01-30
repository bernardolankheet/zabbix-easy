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
	"sync"
	"os"
)

// Debug flag controlled by ENV APP_DEBUG (true/1/yes to enable)
var debugApi bool = false
// CHECKTRENDTIME controls how far back getLastTrend queries trends.
// Format examples: 30d, 1d, 12h, 10m (days/hours/minutes). Defaults to 30d.
var checkTrendDurationSeconds int64 = 30 * 24 * 60 * 60

func parseCheckTrendEnv() {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("CHECKTRENDTIME")))
	if v == "" {
		return
	}
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

// Simple cache for item lookups: key is key+"|"+hostid -> map[string]interface{}
var itemLookupCache sync.Map

func initHttpClient() {
	if httpClient != nil {
		return
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	httpClient = &http.Client{Transport: tr, Timeout: 20 * time.Second}
}

// Função para coletar e gerar requisições ao Zabbix API
func zabbixApiRequest(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method": method,
		"params": params,
		"id": 1,
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
	var resp *http.Response
	start := time.Now()
	resp, err := httpClient.Post(apiUrl, "application/json", strings.NewReader(string(reqBytes)))
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

// ... (rest of the file omitted for brevity, full content copied from go-report/cmd/app/main.go)
