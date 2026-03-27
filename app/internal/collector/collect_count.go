package collector

import "fmt"

// CollectCount performs a JSON-RPC call with `countOutput: true` and returns
// the numeric result as int. It uses the provided ApiRequester.
func CollectCount(apiUrl, token, method string, params map[string]interface{}, req ApiRequester) (int, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	params["countOutput"] = true
	resp, err := req(apiUrl, token, method, params)
	if err != nil {
		return 0, err
	}
	if r, ok := resp["result"]; ok {
		switch t := r.(type) {
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
