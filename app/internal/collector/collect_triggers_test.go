package collector

import (
	"errors"
	"testing"
)

func TestCollectTriggers_Success(t *testing.T) {
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"result": []interface{}{
			map[string]interface{}{"triggerid": "1", "description": "t1", "error": "err1", "hosts": []interface{}{map[string]interface{}{"hostid": "10", "name": "host1"}}},
			map[string]interface{}{"triggerid": "2", "description": "t2", "error": "", "hosts": []interface{}{map[string]interface{}{"hostid": "11", "name": "host2"}}},
		}}, nil
	}
	arr, err := CollectTriggers("u", "t", 1, fake)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(arr))
	}
}

func TestCollectTriggers_Error(t *testing.T) {
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		return nil, errors.New("fail")
	}
	_, err := CollectTriggers("u", "t", 1, fake)
	if err == nil {
		t.Fatalf("expected error")
	}
}
