package collector

import (
	"strings"
)

// HostResolveResult describes the outcome of a host filter lookup.
type HostResolveResult struct {
	Status string         `json:"status"` // ok, ambiguous, not_found, empty
	Host   ResolvedHost   `json:"host,omitempty"`
	Hosts  []ResolvedHost `json:"hosts,omitempty"`
}

// ResolveHostsByFilter inspects hosts and returns match status without failing on ambiguity.
func ResolveHostsByFilter(hosts []map[string]interface{}, hostFilter string) HostResolveResult {
	filter := strings.TrimSpace(hostFilter)
	if filter == "" {
		return HostResolveResult{Status: "empty"}
	}
	normalizedFilter := strings.ToLower(filter)

	for _, h := range hosts {
		if fmtValue(h["hostid"]) == filter {
			return HostResolveResult{Status: "ok", Host: hostFromMap(h)}
		}
	}

	for _, h := range hosts {
		tech := strings.ToLower(strings.TrimSpace(fmtValue(h["host"])))
		vis := strings.ToLower(strings.TrimSpace(fmtValue(h["name"])))
		if tech == normalizedFilter || vis == normalizedFilter {
			return HostResolveResult{Status: "ok", Host: hostFromMap(h)}
		}
	}

	var partial []ResolvedHost
	seen := map[string]bool{}
	for _, h := range hosts {
		tech := strings.ToLower(strings.TrimSpace(fmtValue(h["host"])))
		vis := strings.ToLower(strings.TrimSpace(fmtValue(h["name"])))
		if tech == "" && vis == "" {
			continue
		}
		if strings.Contains(tech, normalizedFilter) || strings.Contains(vis, normalizedFilter) {
			rh := hostFromMap(h)
			if seen[rh.HostID] {
				continue
			}
			seen[rh.HostID] = true
			partial = append(partial, rh)
		}
	}
	if len(partial) == 1 {
		return HostResolveResult{Status: "ok", Host: partial[0]}
	}
	if len(partial) > 1 {
		return HostResolveResult{Status: "ambiguous", Hosts: partial}
	}
	return HostResolveResult{Status: "not_found"}
}
