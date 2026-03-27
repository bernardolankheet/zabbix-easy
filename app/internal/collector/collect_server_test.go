package collector

import "testing"

func TestCollectZabbixVersion(t *testing.T) {
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method != "apiinfo.version" {
			t.Fatalf("unexpected method: %s", method)
		}
		return map[string]interface{}{"result": "6.0.1"}, nil
	}
	v, err := CollectZabbixVersion("u", fake)
	if err != nil {
		t.Fatal(err)
	}
	if v != "6.0.1" {
		t.Fatalf("unexpected version %s", v)
	}
}
