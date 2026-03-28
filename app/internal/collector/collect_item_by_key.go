package collector

// CollectItemByKey finds an item by exact key_ and optional hostid. Returns
// the first matching item or nil if not found.
func CollectItemByKey(apiUrl, token, key, hostid string, req ApiRequester) (map[string]interface{}, error) {
	params := map[string]interface{}{
		"output": []string{"itemid", "hostid", "name", "key_", "value_type"},
		"filter": map[string]interface{}{"key_": key},
		"limit":  1,
	}
	if hostid != "" {
		params["hostids"] = hostid
	}
	resp, err := req(apiUrl, token, "item.get", params)
	if err != nil {
		return nil, err
	}
	if r, ok := resp["result"]; ok {
		if arr, ok2 := r.([]interface{}); ok2 && len(arr) > 0 {
			if m, ok3 := arr[0].(map[string]interface{}); ok3 {
				return m, nil
			}
		}
	}
	return nil, nil
}
