package zabbix

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	collector "go-zabbix-report/internal/collector"
)

// Config holds client-level settings.
type Config struct {
	Debug              bool
	TrendWindowSeconds int64
	TimeoutSeconds     int
}

// Client communicates with the Zabbix JSON-RPC API.
type Client struct {
	debug              bool
	useBearerAuth      bool
	trendWindowSeconds int64
	httpClient         *http.Client
	httpTransport      *http.Transport
	itemLookupCache    sync.Map
}

// NewClient creates a Zabbix API client.
func NewClient(cfg Config) *Client {
	timeout := 60
	if cfg.TimeoutSeconds > 0 {
		timeout = cfg.TimeoutSeconds
	} else if v := os.Getenv("API_TIMEOUT_SECONDS"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			timeout = n
		}
	}
	c := &Client{
		debug:              cfg.Debug,
		trendWindowSeconds: cfg.TrendWindowSeconds,
		httpTransport: &http.Transport{
			TLSClientConfig:     tlsConfig(),
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	c.httpClient = &http.Client{
		Transport: c.httpTransport,
		Timeout:   time.Duration(timeout) * time.Second,
	}
	return c
}

func tlsConfig() *tls.Config {
	insecure := true
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("ZABBIX_TLS_VERIFY"))); v == "true" || v == "1" || v == "yes" {
		insecure = false
	}
	return &tls.Config{InsecureSkipVerify: insecure}
}

// SetDebug toggles verbose request/response logging.
func (c *Client) SetDebug(on bool) { c.debug = on }

// SetBearerAuth toggles Zabbix 7.2+ Authorization header auth.
func (c *Client) SetBearerAuth(on bool) { c.useBearerAuth = on }

// SetTrendWindowSeconds updates the trend/history lookback window.
func (c *Client) SetTrendWindowSeconds(sec int64) { c.trendWindowSeconds = sec }

// APIRequest performs a JSON-RPC call to the Zabbix API.
func (c *Client) APIRequest(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	if token != "" && !c.useBearerAuth {
		req["auth"] = token
	}
	reqBytes, _ := json.Marshal(req)
	if c.debug {
		log.Printf("[ZABBIX DEBUG] Request %s -> %s", method, string(reqBytes))
	}

	const maxRetries = 3
	var resp *http.Response
	var err error
	start := time.Now()
	for attempt := 1; attempt <= maxRetries; attempt++ {
		httpReq, reqErr := http.NewRequest("POST", apiUrl, strings.NewReader(string(reqBytes)))
		if reqErr != nil {
			err = reqErr
			break
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.useBearerAuth && token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+token)
		}
		if attempt > 1 {
			httpReq.Header.Set("Connection", "close")
		}
		resp, err = c.httpClient.Do(httpReq)
		if err == nil {
			break
		}
		if isDeadlineError(err) {
			log.Printf("[ZABBIX API] %s timeout (tentativa %d/%d): %v", method, attempt, maxRetries, err)
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
	if c.debug {
		b := string(bodyBytes)
		if len(b) > 4096 {
			b = b[:4096] + "...(truncated)"
		}
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

func isDeadlineError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "Client.Timeout exceeded") ||
		strings.Contains(s, "i/o timeout")
}

// GetItemByKey finds an item by exact key_, optionally scoped to a host.
func (c *Client) GetItemByKey(apiUrl, token, key, hostid string) (map[string]interface{}, error) {
	cacheKey := key + "|" + hostid
	if v, ok := c.itemLookupCache.Load(cacheKey); ok {
		if m, ok2 := v.(map[string]interface{}); ok2 {
			return m, nil
		}
	}
	params := map[string]interface{}{
		"output": []string{"itemid", "hostid", "name", "key_", "value_type"},
		"filter": map[string]interface{}{"key_": key},
		"limit":  1,
	}
	if hostid != "" {
		params["hostids"] = hostid
	}
	arr, err := collector.CollectRawList(apiUrl, token, "item.get", params, c.APIRequest)
	if err != nil {
		return nil, err
	}
	if len(arr) > 0 {
		m := arr[0]
		c.itemLookupCache.Store(cacheKey, m)
		return m, nil
	}
	return nil, nil
}

// GetLastHistoryValue returns the most recent history point for an item.
func (c *Client) GetLastHistoryValue(apiUrl, token, itemid string, historyType int) (string, error) {
	params := map[string]interface{}{
		"output":    "extend",
		"history":   historyType,
		"itemids":   itemid,
		"sortfield": "clock",
		"sortorder": "DESC",
		"limit":     1,
	}
	arr, err := collector.CollectRawList(apiUrl, token, "history.get", params, c.APIRequest)
	if err != nil {
		return "", err
	}
	if len(arr) > 0 {
		return fmt.Sprintf("%v", arr[0]["value"]), nil
	}
	return "", nil
}

// GetHistoryStats computes min/avg/max from history for one item.
func (c *Client) GetHistoryStats(apiUrl, token, itemid string, histType int, days int) (map[string]interface{}, error) {
	now := time.Now().Unix()
	from := c.timeFrom(now, days)
	params := map[string]interface{}{
		"output":    []string{"value"},
		"history":   histType,
		"itemids":   []string{itemid},
		"time_from": from,
		"time_till": now,
		"sortfield": "clock",
		"sortorder": "ASC",
		"limit":     2000,
	}
	arr, err := collector.CollectRawList(apiUrl, token, "history.get", params, c.APIRequest)
	if err != nil {
		return nil, err
	}
	return aggregateValues(arr), nil
}

// GetLastTrend fetches and aggregates trend data for one item.
func (c *Client) GetLastTrend(apiUrl, token, itemid string, days int) (map[string]interface{}, error) {
	now := time.Now().Unix()
	from := c.timeFrom(now, days)
	params := map[string]interface{}{
		"output":    []string{"itemid", "clock", "value_min", "value_avg", "value_max"},
		"itemids":   []string{itemid},
		"time_from": from,
		"time_to":   now,
	}
	arr, err := collector.CollectRawList(apiUrl, token, "trend.get", params, c.APIRequest)
	if err != nil {
		return nil, err
	}
	if len(arr) == 0 {
		return nil, nil
	}
	type aggState struct {
		vmin, vmax float64
		vavgSum    float64
		count      int
	}
	var agg *aggState
	parseF := func(row map[string]interface{}, k string) (float64, bool) {
		if v, ok2 := row[k]; ok2 {
			if f, e := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); e == nil {
				return f, true
			}
		}
		return 0, false
	}
	for _, row := range arr {
		if row == nil {
			continue
		}
		vmin, ok1 := parseF(row, "value_min")
		vavg, ok2 := parseF(row, "value_avg")
		vmax, ok3 := parseF(row, "value_max")
		if !ok1 && !ok2 && !ok3 {
			continue
		}
		if agg == nil {
			agg = &aggState{vmin: vmin, vmax: vmax, vavgSum: vavg, count: 1}
		} else {
			if ok1 && vmin < agg.vmin {
				agg.vmin = vmin
			}
			if ok3 && vmax > agg.vmax {
				agg.vmax = vmax
			}
			if ok2 {
				agg.vavgSum += vavg
				agg.count++
			}
		}
	}
	if agg == nil {
		return nil, nil
	}
	vavgFinal := 0.0
	if agg.count > 0 {
		vavgFinal = agg.vavgSum / float64(agg.count)
	}
	return map[string]interface{}{
		"value_min": fmt.Sprintf("%f", agg.vmin),
		"value_avg": fmt.Sprintf("%f", vavgFinal),
		"value_max": fmt.Sprintf("%f", agg.vmax),
	}, nil
}

// GetTrendsBulkStats fetches trend stats for multiple items in one call.
func (c *Client) GetTrendsBulkStats(apiUrl, token string, itemids []string) (map[string]map[string]interface{}, error) {
	if len(itemids) == 0 {
		return map[string]map[string]interface{}{}, nil
	}
	now := time.Now().Unix()
	from := c.timeFrom(now, 30)
	params := map[string]interface{}{
		"output":    []string{"itemid", "value_min", "value_avg", "value_max"},
		"itemids":   itemids,
		"time_from": from,
		"time_to":   now,
	}
	arr, err := collector.CollectRawList(apiUrl, token, "trend.get", params, c.APIRequest)
	if err != nil {
		return nil, err
	}
	type aggState struct {
		vmin, vmaxV float64
		vavgSum     float64
		count       int
	}
	agg := map[string]*aggState{}
	for _, row := range arr {
		if row == nil {
			continue
		}
		iid := fmt.Sprintf("%v", row["itemid"])
		parseF := func(k string) (float64, bool) {
			if v, ok2 := row[k]; ok2 {
				if f, e := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); e == nil {
					return f, true
				}
			}
			return 0, false
		}
		vmin, ok1 := parseF("value_min")
		vavg, ok2 := parseF("value_avg")
		vmax, ok3 := parseF("value_max")
		if !ok1 && !ok2 && !ok3 {
			continue
		}
		if agg[iid] == nil {
			agg[iid] = &aggState{vmin: vmin, vmaxV: vmax, vavgSum: vavg, count: 1}
		} else {
			s := agg[iid]
			if ok1 && vmin < s.vmin {
				s.vmin = vmin
			}
			if ok3 && vmax > s.vmaxV {
				s.vmaxV = vmax
			}
			if ok2 {
				s.vavgSum += vavg
				s.count++
			}
		}
	}
	result := map[string]map[string]interface{}{}
	for iid, s := range agg {
		vavg := 0.0
		if s.count > 0 {
			vavg = s.vavgSum / float64(s.count)
		}
		result[iid] = map[string]interface{}{
			"value_min": fmt.Sprintf("%f", s.vmin),
			"value_avg": fmt.Sprintf("%f", vavg),
			"value_max": fmt.Sprintf("%f", s.vmaxV),
		}
	}
	return result, nil
}

// GetHistoryStatsBulkByType computes min/avg/max from history for multiple items grouped by type.
func (c *Client) GetHistoryStatsBulkByType(apiUrl, token string, items map[string]int) (map[string]map[string]interface{}, error) {
	if len(items) == 0 {
		return map[string]map[string]interface{}{}, nil
	}
	byType := map[int][]string{}
	for iid, vt := range items {
		byType[vt] = append(byType[vt], iid)
	}
	now := time.Now().Unix()
	from := c.timeFrom(now, 30)
	result := map[string]map[string]interface{}{}
	var mu sync.Mutex
	for histType, iids := range byType {
		limit := len(iids) * 500
		if limit > 20000 {
			limit = 20000
		}
		params := map[string]interface{}{
			"output":    []string{"itemid", "value"},
			"history":   histType,
			"itemids":   iids,
			"time_from": from,
			"time_till": now,
			"sortfield": "clock",
			"sortorder": "ASC",
			"limit":     limit,
		}
		arr, err := collector.CollectRawList(apiUrl, token, "history.get", params, c.APIRequest)
		if err != nil {
			continue
		}
		type aggS struct{ vals []float64 }
		agg := map[string]*aggS{}
		for _, row := range arr {
			if row == nil {
				continue
			}
			iid := fmt.Sprintf("%v", row["itemid"])
			if f, e := strconv.ParseFloat(fmt.Sprintf("%v", row["value"]), 64); e == nil {
				if agg[iid] == nil {
					agg[iid] = &aggS{}
				}
				agg[iid].vals = append(agg[iid].vals, f)
			}
		}
		mu.Lock()
		for iid, s := range agg {
			if len(s.vals) == 0 {
				continue
			}
			vmin, vmax, sum := s.vals[0], s.vals[0], 0.0
			for _, v := range s.vals {
				if v < vmin {
					vmin = v
				}
				if v > vmax {
					vmax = v
				}
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

func (c *Client) timeFrom(now int64, days int) int64 {
	if c.trendWindowSeconds > 0 {
		return now - c.trendWindowSeconds
	}
	return now - int64(days*24*60*60)
}

func aggregateValues(arr []map[string]interface{}) map[string]interface{} {
	if len(arr) == 0 {
		return nil
	}
	var vals []float64
	for _, m := range arr {
		if m == nil {
			continue
		}
		if v, ok := m["value"]; ok {
			if f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); err == nil {
				vals = append(vals, f)
			}
		}
	}
	if len(vals) == 0 {
		return nil
	}
	vmin, vmax := vals[0], vals[0]
	sum := 0.0
	for _, v := range vals {
		if v < vmin {
			vmin = v
		}
		if v > vmax {
			vmax = v
		}
		sum += v
	}
	return map[string]interface{}{
		"value_min": fmt.Sprintf("%f", vmin),
		"value_avg": fmt.Sprintf("%f", sum/float64(len(vals))),
		"value_max": fmt.Sprintf("%f", vmax),
	}
}
