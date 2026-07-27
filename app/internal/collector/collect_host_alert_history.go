package collector

// CollectHostAlertHistory builds a historical view for a single host over a time range.
func CollectHostAlertHistory(apiUrl, token, hostFilter string, timeFrom, timeTo int64, req ApiRequester) (HostAlertHistory, error) {
	focus, err := CollectHostFocusReport(apiUrl, token, hostFilter, timeFrom, timeTo, req, HostFocusOptions{})
	return focus.AlertHistory, err
}
