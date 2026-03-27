package collector

// CollectProxiesList performs a `proxy.get` with `output: extend` and
// returns the raw list of proxies as []map[string]interface{} for callers
// that need detailed proxy objects.
func CollectProxiesList(apiUrl, token string, req ApiRequester) ([]map[string]interface{}, error) {
	resp, err := req(apiUrl, token, "proxy.get", map[string]interface{}{"output": "extend"})
	if err != nil {
		return nil, err
	}
	r, ok := resp["result"]
	if !ok {
		return nil, nil
	}
	arr, ok := r.([]interface{})
	if !ok {
		return nil, nil
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, nil
}
