package collector

import "testing"

func TestCollectItemByKey(t *testing.T) {
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method != "item.get" {
			t.Fatalf("unexpected method %s", method)
		}
		return map[string]interface{}{
			"result": []interface{}{map[string]interface{}{"itemid": "42", "key_": "zabbix[requiredperformance]", "hostid": "10"}},
		}, nil
	}
	it, err := CollectItemByKey("u", "t", "zabbix[requiredperformance]", "10", fake)
	if err != nil {
		t.Fatal(err)
	}
	if it == nil {
		t.Fatal("expected item, got nil")
	}
	if it["itemid"] != "42" {
		t.Fatalf("unexpected itemid %v", it["itemid"])
	}
}
