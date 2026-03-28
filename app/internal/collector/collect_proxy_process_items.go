package collector

import (
	"fmt"
	"sort"
	"strings"
)

// CollectProxyProcessItems fetches items of type internal (type 5 or 18) for a
// proxy host and matches them to provided process names using nameToWildcard
// matching against both key_ and name fields. Returns map[nameLower] -> item.
func CollectProxyProcessItems(apiUrl, token string, names []string, hostid string, req ApiRequester) (map[string]map[string]interface{}, error) {
	if len(names) == 0 || hostid == "" {
		return map[string]map[string]interface{}{}, nil
	}
	params := map[string]interface{}{
		"output":  []string{"itemid", "hostid", "name", "key_", "value_type"},
		"hostids": hostid,
		"filter":  map[string]interface{}{"type": []int{5, 18}},
	}
	resp, err := req(apiUrl, token, "item.get", params)
	if err != nil {
		return nil, err
	}
	type nameEntry struct {
		norm    string
		words   int
		pattern string
	}
	entries := make([]nameEntry, len(names))
	for i, n := range names {
		entries[i] = nameEntry{norm: strings.ToLower(strings.TrimSpace(n)), words: len(strings.Fields(n)), pattern: nameToWildcard(n)}
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
