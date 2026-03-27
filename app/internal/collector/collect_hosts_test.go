package collector

import "testing"

func TestCollectHosts(t *testing.T) {
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method != "host.get" {
			t.Fatalf("unexpected method: %s", method)
		}
		return map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{"hostid": "101", "host": "server1", "name": "Server One", "status": "0"},
			},
		}, nil
	}
	hosts, err := CollectHosts("u", "tok", fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0]["hostid"] != "101" {
		t.Fatalf("unexpected hostid %v", hosts[0]["hostid"])
	}
}
