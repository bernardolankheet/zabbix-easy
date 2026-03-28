package collector

import (
	"errors"
	"testing"
)

func TestCollectProxies_Success(t *testing.T) {
	// fake requester returns two proxies
	fake := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{"name": "p1", "state": "0"},
				map[string]interface{}{"name": "p2", "operating_mode": 0},
			},
		}, nil
	}

	sum, err := CollectProxies("http://x", "tok", 7, fake)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sum.Total != 2 {
		t.Fatalf("expected total 2, got %d", sum.Total)
	}
	if sum.Unknown != 1 {
		t.Fatalf("expected unknown 1, got %d", sum.Unknown)
	}
}

func TestCollectProxies_Error(t *testing.T) {
	fakeErr := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		return nil, errors.New("api down")
	}
	_, err := CollectProxies("x", "t", 7, fakeErr)
	if err == nil {
		t.Fatalf("expected error")
	}
}
