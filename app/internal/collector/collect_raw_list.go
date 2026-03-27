package collector

// CollectRawList executes the given JSON-RPC method with params and returns
// the `result` as a slice of maps for callers that need to iterate results.
func CollectRawList(apiUrl, token, method string, params map[string]interface{}, req ApiRequester) ([]map[string]interface{}, error) {
	resp, err := req(apiUrl, token, method, params)
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
