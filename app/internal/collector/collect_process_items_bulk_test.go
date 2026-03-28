package collector

import "testing"

func TestCollectProcessItemsBulk(t *testing.T) {
	names := []string{"agent poller", "history syncer"}
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method != "item.get" {
			t.Fatalf("unexpected method %s", method)
		}
		return map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{"itemid": "1", "key_": "zabbix[process,agent poller]", "name": "agent poller"},
				map[string]interface{}{"itemid": "2", "key_": "zabbix[process,history syncer]", "name": "history syncer"},
			},
		}, nil
	}
	res, err := CollectProcessItemsBulk("u", "t", names, "", fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res))
	}
}
