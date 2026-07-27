package collector

import (
	"testing"
	"time"
)

func TestCollectHostAlertHistory(t *testing.T) {
	base := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		switch method {
		case "host.get":
			return map[string]interface{}{"result": []interface{}{
				map[string]interface{}{"hostid": "1001", "host": "server1", "name": "Server One"},
			}}, nil
		case "event.get":
			return map[string]interface{}{"result": []interface{}{
				map[string]interface{}{"eventid": "1", "clock": base.Add(24 * time.Hour).Unix(), "value": "1"},
				map[string]interface{}{"eventid": "2", "clock": base.Add(24 * time.Hour).Unix(), "value": "0"},
				map[string]interface{}{"eventid": "3", "clock": base.Add(48 * time.Hour).Unix(), "value": "1"},
			}}, nil
		case "item.get":
			return map[string]interface{}{"result": []interface{}{}}, nil
		default:
			t.Fatalf("unexpected method: %s", method)
			return nil, nil
		}
	}

	history, err := CollectHostAlertHistory("u", "tok", "server1", base.Add(-24*time.Hour).Unix(), base.Add(72*time.Hour).Unix(), fake)
	if err != nil {
		t.Fatal(err)
	}
	if history.HostID != "1001" {
		t.Fatalf("unexpected hostid: %s", history.HostID)
	}
	if history.TotalEvents != 3 {
		t.Fatalf("expected 3 events, got %d", history.TotalEvents)
	}
	if len(history.Data) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(history.Data))
	}
	if history.Data[0].Problems != 1 || history.Data[0].Resolved != 1 {
		t.Fatalf("unexpected first bucket values: %+v", history.Data[0])
	}
	if history.Data[1].Problems != 1 || history.Data[1].Resolved != 0 {
		t.Fatalf("unexpected second bucket values: %+v", history.Data[1])
	}
}
