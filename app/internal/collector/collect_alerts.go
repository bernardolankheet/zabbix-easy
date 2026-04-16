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
