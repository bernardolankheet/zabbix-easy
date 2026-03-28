package collector

import (
	"fmt"
	"sort"
	"strings"
)

// helper: convert name to wildcard pattern
func nameToWildcard(name string) string {
	words := strings.Fields(strings.TrimSpace(name))
	return "*" + strings.Join(words, "*") + "*"
}

// helper: simple wildcard (only '*') match, case-insensitive
func wildcardMatch(pattern, s string) bool {
	parts := strings.Split(strings.ToLower(pattern), "*")
	sl := strings.ToLower(s)
	pos := 0
	for _, p := range parts {
		if p == "" {
			continue
		}
		idx := strings.Index(sl[pos:], p)
		if idx < 0 {
			return false
		}
		pos += idx + len(p)
	}
	return true
}

// CollectProcessItemsBulk performs the same semantics as the previous
// getProcessItemsBulk in main.go: single item.get with search wildcards and
// client-side resolution by specificity (more words wins).
func CollectProcessItemsBulk(apiUrl, token string, names []string, hostid string, req ApiRequester) (map[string]map[string]interface{}, error) {
	if len(names) == 0 {
		return map[string]map[string]interface{}{}, nil
	}
	patterns := make([]string, len(names))
	for i, n := range names {
		patterns[i] = nameToWildcard(n)
	}
	params := map[string]interface{}{
		"output":                 []string{"itemid", "hostid", "name", "key_", "value_type"},
		"search":                 map[string]interface{}{"key_": patterns},
		"searchByAny":            true,
		"searchWildcardsEnabled": true,
	}
	if hostid != "" {
		params["hostids"] = hostid
	}
	resp, err := req(apiUrl, token, "item.get", params)
	if err != nil {
		return nil, err
	}
	// build entries sorted by word count desc
	type nameEntry struct {
		norm    string
		words   int
		pattern string
	}
	entries := make([]nameEntry, len(names))
	for i, n := range names {
		entries[i] = nameEntry{norm: strings.ToLower(strings.TrimSpace(n)), words: len(strings.Fields(n)), pattern: patterns[i]}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].words > entries[j].words })
	result := map[string]map[string]interface{}{}
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		for _, raw := range arr {
			item, _ := raw.(map[string]interface{})
			if item == nil {
				continue
			}
			itemKey := fmt.Sprintf("%v", item["key_"])
			itemName := fmt.Sprintf("%v", item["name"])
			for _, e := range entries {
				if wildcardMatch(e.pattern, itemKey) || wildcardMatch(e.pattern, itemName) {
					if _, exists := result[e.norm]; !exists {
						result[e.norm] = item
					}
					break
				}
			}
		}
	}
	return result, nil
}
