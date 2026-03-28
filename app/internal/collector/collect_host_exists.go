package collector

// CollectHostExists returns true if a host with the given hostid exists.
func CollectHostExists(apiUrl, token, hostid string, req ApiRequester) (bool, error) {
	if hostid == "" {
		return false, nil
	}
	params := map[string]interface{}{
		"output":  []string{"hostid"},
		"hostids": []string{hostid},
	}
	resp, err := req(apiUrl, token, "host.get", params)
	if err != nil {
		return false, err
	}
	if r, ok := resp["result"]; ok {
		if arr, ok2 := r.([]interface{}); ok2 && len(arr) > 0 {
			return true, nil
		}
	}
	return false, nil
}
