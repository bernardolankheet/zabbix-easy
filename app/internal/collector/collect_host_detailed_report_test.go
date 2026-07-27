package collector

import (
	"encoding/json"
	"testing"
)

func TestCollectHostDetailedReport_HostNotFound(t *testing.T) {
	mock := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		if method == "host.get" {
			return map[string]interface{}{"result": []interface{}{}}, nil
		}
		return map[string]interface{}{"result": []interface{}{}}, nil
	}
	_, err := CollectHostDetailedReport("u", "t", "nope", 1600000000, 1600003600, mock)
	if err == nil {
		t.Fatal("expected host not found error")
	}
}

func TestCollectHostDetailedReport_BasicFlow(t *testing.T) {
	mock := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		switch method {
		case "host.get":
			return map[string]interface{}{"result": []interface{}{map[string]interface{}{"hostid": "1", "host": "myhost"}}}, nil
		case "event.get":
			ev := map[string]interface{}{"eventid": "e1", "clock": "1600000100", "value": "1", "name": "trigger1"}
			return map[string]interface{}{"result": []interface{}{ev}}, nil
		case "item.get":
			it := map[string]interface{}{"itemid": "10", "hostid": "1", "key_": "system.cpu.util", "name": "CPU usage", "value_type": "0"}
			return map[string]interface{}{"result": []interface{}{it}}, nil
		case "history.get":
			p1 := map[string]interface{}{"clock": "1600000050", "value": "10"}
			p2 := map[string]interface{}{"clock": "1600000150", "value": "20"}
			return map[string]interface{}{"result": []interface{}{p1, p2}}, nil
		case "trend.get":
			return map[string]interface{}{"result": []interface{}{}}, nil
		default:
			return map[string]interface{}{"result": []interface{}{}}, nil
		}
	}
	r, err := CollectHostDetailedReport("u", "t", "myhost", 1600000000, 1600001000, mock)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if r.Status != "ok" && r.Status != "no_events" {
		t.Fatalf("unexpected status: %s", r.Status)
	}
	if len(r.Metrics) == 0 {
		t.Fatalf("expected metrics, got none")
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("report not JSON serializable: %v", err)
	}
}
