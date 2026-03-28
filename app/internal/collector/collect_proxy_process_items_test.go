package collector

import "testing"

func TestCollectProxyProcessItems(t *testing.T) {
	names := []string{"agent poller", "sender"}
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method != "item.get" {
			t.Fatalf("unexpected method %s", method)
		}
		return map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{"itemid": "11", "key_": "zabbix[process,agent poller]", "name": "agent poller"},
				map[string]interface{}{"itemid": "12", "key_": "zabbix[process,data sender]", "name": "data sender"},
			},
		}, nil
	}
	res, err := CollectProxyProcessItems("u", "t", names, "101", fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res))
	}
}
