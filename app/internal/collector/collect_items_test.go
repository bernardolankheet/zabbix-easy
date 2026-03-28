package collector

import "testing"

func TestCollectItems(t *testing.T) {
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method != "item.get" {
			t.Fatalf("unexpected method: %s", method)
		}
		return map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{"itemid": "1", "name": "cpu", "hostid": "101", "key_": "system.cpu", "value_type": "0"},
			},
		}, nil
	}
	items, err := CollectItems("u", "tok", fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["itemid"] != "1" {
		t.Fatalf("unexpected itemid %v", items[0]["itemid"])
	}
}
