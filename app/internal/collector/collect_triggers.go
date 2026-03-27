package collector

// CollectTriggers fetches triggers in the given state (e.g., state=1 for Unknown)
// using the provided ApiRequester and returns the slice of triggers as
// []map[string]interface{} for further aggregation by the caller.
func CollectTriggers(apiUrl, token string, state int, req ApiRequester) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output":      []string{"triggerid", "description", "error"},
		"selectHosts": []string{"hostid", "name"},
		"filter":      map[string]interface{}{"state": state},
		"monitored":   true,
	}
	resp, err := req(apiUrl, token, "trigger.get", params)
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
