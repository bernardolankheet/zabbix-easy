package collector

// CollectItems performs an `item.get` call and returns the list of items
// as []map[string]interface{} for callers to aggregate or inspect.
func CollectItems(apiUrl, token string, req ApiRequester) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output": []string{"itemid", "name", "hostid", "key_", "value_type"},
	}
	resp, err := req(apiUrl, token, "item.get", params)
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
