package collector

import (
	"fmt"
	"strings"
)

// serverOnlyProcesses lists internal processes that only the Zabbix *server*
// runs — a proxy never has them. They are what tells the server's
// self-monitoring host apart from a proxy's, since both carry
// `zabbix[process,...]` items. The names are stable across Zabbix 6.0–8.0.
var serverOnlyProcesses = []string{
	"escalator",
	"alerter",
	"alert manager",
	"service manager",
	"lld manager",
	"report manager",
	"proxy poller",
}

// CollectServerHostId discovers the hostid of the host that carries the Zabbix
// Server's internal process items (`zabbix[process,...]`), so the report does
// not depend on a hardcoded id — hostids are assigned per installation, and the
// conventional 10084 only holds on a fresh install.
//
// One `item.get` for every monitored process item, grouped by host. Proxies
// match too, so the winner is the host with the most server-only processes,
// falling back to the host with the most process items overall.
//
// Returns "" (no error) when nothing matches — the caller decides what to do,
// typically keeping whatever ZABBIX_SERVER_HOSTID provided.
func CollectServerHostId(apiUrl, token string, req ApiRequester) (string, error) {
	params := map[string]interface{}{
		"output": []string{"itemid", "key_"},
		// substring match — sem searchWildcardsEnabled o Zabbix faz LIKE %valor%,
		// que é o que queremos e evita a pegadinha de exigir '*' explícito.
		"search":      map[string]interface{}{"key_": "zabbix[process,"},
		"monitored":   true,
		"selectHosts": []string{"hostid", "host"},
	}
	resp, err := req(apiUrl, token, "item.get", params)
	if err != nil {
		return "", err
	}
	r, ok := resp["result"]
	if !ok {
		return "", nil
	}
	arr, ok := r.([]interface{})
	if !ok {
		return "", nil
	}

	type hostScore struct {
		total      int
		serverOnly int
	}
	scores := map[string]*hostScore{}

	for _, raw := range arr {
		item, _ := raw.(map[string]interface{})
		if item == nil {
			continue
		}
		hosts, _ := item["hosts"].([]interface{})
		if len(hosts) == 0 {
			continue
		}
		h, _ := hosts[0].(map[string]interface{})
		if h == nil {
			continue
		}
		hostid := fmt.Sprintf("%v", h["hostid"])
		if hostid == "" || hostid == "<nil>" {
			continue
		}
		if scores[hostid] == nil {
			scores[hostid] = &hostScore{}
		}
		scores[hostid].total++

		key := strings.ToLower(fmt.Sprintf("%v", item["key_"]))
		for _, p := range serverOnlyProcesses {
			if strings.Contains(key, p) {
				scores[hostid].serverOnly++
				break
			}
		}
	}

	best := ""
	for hostid, s := range scores {
		if best == "" {
			best = hostid
			continue
		}
		b := scores[best]
		if s.serverOnly != b.serverOnly {
			if s.serverOnly > b.serverOnly {
				best = hostid
			}
			continue
		}
		if s.total > b.total {
			best = hostid
			continue
		}
		// Empate total: menor hostid, só para a escolha ser determinística
		// (a ordem de iteração de um map em Go é aleatória).
		if s.total == b.total && hostid < best {
			best = hostid
		}
	}
	return best, nil
}
