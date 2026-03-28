package collector

// ApiRequester is a function that performs a JSON-RPC request against the
// Zabbix API. It matches the shape used in the app: receives apiUrl, token,
// method and params, and returns the decoded JSON as map[string]interface{}.
type ApiRequester func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error)

// CollectProxies performs a `proxy.get` via the provided requester and
// returns a ProxySummary. majorV is used by callers if needed for
// compatibility; collector logic attempts to be version-agnostic.
func CollectProxies(apiUrl, token string, majorV int, req ApiRequester) (ProxySummary, error) {
	resp, err := req(apiUrl, token, "proxy.get", map[string]interface{}{})
	if err != nil {
		return ProxySummary{}, err
	}
	// Expect resp["result"] to be a slice of objects
	r, ok := resp["result"]
	if !ok {
		return ProxySummary{}, nil
	}
	slice, ok := r.([]interface{})
	if !ok {
		return ProxySummary{}, nil
	}
	proxies := make([]map[string]interface{}, 0, len(slice))
	for _, it := range slice {
		if m, ok := it.(map[string]interface{}); ok {
			proxies = append(proxies, m)
		}
	}
	sum := SummarizeProxies(proxies)
	// preserve total fallback behavior: if proxies slice empty, total remains 0
	if sum.Total == 0 {
		// try to read countOutput from resp (some callers may return summary counts)
		if cnt, ok := resp["count"]; ok {
			switch t := cnt.(type) {
			case float64:
				sum.Total = int(t)
			case int:
				sum.Total = t
			}
		}
	}
	return sum, nil
}
