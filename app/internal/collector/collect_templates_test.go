package collector

import "testing"

func TestCollectTemplates(t *testing.T) {
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method != "template.get" {
			t.Fatalf("unexpected method: %s", method)
		}
		return map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{"templateid": "201", "name": "Template OS", "host": "Template OS"},
			},
		}, nil
	}
	ts, err := CollectTemplates("u", "tok", fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 {
		t.Fatalf("expected 1 template, got %d", len(ts))
	}
	if ts[0]["templateid"] != "201" {
		t.Fatalf("unexpected templateid %v", ts[0]["templateid"])
	}
}
