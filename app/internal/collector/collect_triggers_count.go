package collector

import "fmt"

// CollectTriggersCount returns the total number of triggers (countOutput)
// using the provided ApiRequester. It keeps the count logic centralized.
func CollectTriggersCount(apiUrl, token string, req ApiRequester) (int, error) {
	resp, err := req(apiUrl, token, "trigger.get", map[string]interface{}{"countOutput": true})
	if err != nil {
		return 0, err
	}
	if n, ok := resp["result"]; ok {
		switch t := n.(type) {
		case float64:
			return int(t), nil
		case int:
			return t, nil
		case string:
			var v int
			_, err := fmt.Sscanf(t, "%d", &v)
			if err == nil {
				return v, nil
			}
		}
	}
	return 0, nil
}
