package collector

// CollectActions fetches all trigger-based actions (eventsource=0) using the
// provided ApiRequester and returns the slice of actions as []map[string]interface{}.
func CollectActions(apiUrl, token string, req ApiRequester) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output": []string{"actionid", "name", "status"},
		"filter": map[string]interface{}{"eventsource": 0},
	}
	return CollectRawList(apiUrl, token, "action.get", params, req)
}
