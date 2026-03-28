package collector

import "fmt"

// ProxySummary summarizes proxy counts and names by state/mode.
type ProxySummary struct {
	Total        int      `json:"total"`
	Unknown      int      `json:"unknown"`
	Offline      int      `json:"offline"`
	Active       int      `json:"active"`
	Passive      int      `json:"passive"`
	UnknownNames []string `json:"unknown_names"`
	OfflineNames []string `json:"offline_names"`
}

// SummarizeProxies computes a ProxySummary from the raw proxies slice as returned
// by the Zabbix `proxy.get` JSON-RPC method. The function is defensive with
// types: it stringifies fields before comparison to support numeric or string
// representations.
func SummarizeProxies(proxies []map[string]interface{}) ProxySummary {
	var s ProxySummary
	s.Total = len(proxies)
	for _, p := range proxies {
		name := fmtValue(p["name"])
		if name == "" {
			name = fmtValue(p["host"])
		}

		// Determine state: prefer `state` field (0=unknown,1=offline,2=online),
		// fall back to `status` where 5=active,6=passive in some older payloads.
		state := fmtValue(p["state"])
		status := fmtValue(p["status"])
		operatingMode := fmtValue(p["operating_mode"]) // 0 active, 1 passive

		// Unknown
		if state == "0" || state == "unknown" {
			s.Unknown++
			if name != "" {
				s.UnknownNames = append(s.UnknownNames, name)
			}
			continue
		}

		// Offline
		if state == "1" || state == "offline" {
			s.Offline++
			if name != "" {
				s.OfflineNames = append(s.OfflineNames, name)
			}
			continue
		}

		// Online / determine active/passive
		// operating_mode: 0 active, 1 passive
		if operatingMode == "0" {
			s.Active++
			continue
		}
		if operatingMode == "1" {
			s.Passive++
			continue
		}

		// Fallback to status codes (older formats): 5=active, 6=passive
		if status == "5" || status == "active" {
			s.Active++
			continue
		}
		if status == "6" || status == "passive" {
			s.Passive++
			continue
		}

		// If we couldn't classify, leave it in total but don't increment active/passive
	}
	return s
}

func fmtValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		// JSON numbers are float64 when decoded into interface{}
		// but often are integers, format without decimal when appropriate.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
