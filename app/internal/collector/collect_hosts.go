package collector

// CollectHosts performs a `host.get` and returns hosts as []map[string]interface{}.
func CollectHosts(apiUrl, token string, req ApiRequester) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output": []string{"hostid", "host", "name", "status"},
	}
	resp, err := req(apiUrl, token, "host.get", params)
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
