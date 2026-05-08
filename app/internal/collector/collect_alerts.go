package collector

// CollectAlertCount returns the number of alerts for a given action within
// a time window. If alertStatus >= 0, the count is filtered by that status
// (e.g. 2 = failed). If alertStatus < 0, all statuses are included (total).
// timeFrom is a Unix timestamp; only alerts created after this time are counted.
func CollectAlertCount(apiUrl, token string, actionid string, alertStatus int, timeFrom int64, req ApiRequester) (int, error) {
	filter := map[string]interface{}{
		"actionid": actionid,
	}
	if alertStatus >= 0 {
		filter["status"] = alertStatus
	}
	params := map[string]interface{}{
		"output":    "extend",
		"filter":    filter,
		"time_from": timeFrom,
	}
	return CollectCount(apiUrl, token, "alert.get", params, req)
}

// CollectFailedAlertDetails fetches failed alerts (status=2) with details
// including mediatypeid, error message and actionid for the given time window.
func CollectFailedAlertDetails(apiUrl, token string, timeFrom int64, req ApiRequester) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output":    []string{"alertid", "mediatypeid", "error", "actionid"},
		"filter":    map[string]interface{}{"status": 2},
		"time_from": timeFrom,
		"sortfield": "alertid",
		"sortorder": "DESC",
		"limit":     5000,
	}
	return CollectRawList(apiUrl, token, "alert.get", params, req)
}
