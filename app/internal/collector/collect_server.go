package collector

// CollectZabbixVersion queries the API info method and returns the version
// string. Some callers may use it to decide behavior for compatibility.
func CollectZabbixVersion(apiUrl string, req ApiRequester) (string, error) {
	// apiinfo.version typically does not require a token, but our requester
	// signature includes token; pass empty string.
	resp, err := req(apiUrl, "", "apiinfo.version", map[string]interface{}{})
	if err != nil {
		return "", err
	}
	if v, ok := resp["result"]; ok {
		if s, ok := v.(string); ok {
			return s, nil
		}
	}
	return "", nil
}
