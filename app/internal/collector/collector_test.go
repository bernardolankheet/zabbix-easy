package collector

import "testing"

func TestSummarizeProxies(t *testing.T) {
	proxies := []map[string]interface{}{
		{"name": "p1", "state": "0"},
		{"name": "p2", "state": 1},
		{"name": "p3", "operating_mode": 0},
		{"name": "p4", "operating_mode": 1},
		{"name": "p5", "status": 5},
		{"name": "p6", "status": 6},
		{"host": "p7", "state": float64(2)},
	}

	sum := SummarizeProxies(proxies)

	if sum.Total != 7 {
		t.Fatalf("expected total 7, got %d", sum.Total)
	}
	if sum.Unknown != 1 {
		t.Fatalf("expected unknown 1, got %d", sum.Unknown)
	}
	if sum.Offline != 1 {
		t.Fatalf("expected offline 1, got %d", sum.Offline)
	}
	if sum.Active != 2 { // p3 and p5
		t.Fatalf("expected active 2, got %d", sum.Active)
	}
	if sum.Passive != 2 { // p4 and p6
		t.Fatalf("expected passive 2, got %d", sum.Passive)
	}
	if len(sum.UnknownNames) != 1 || sum.UnknownNames[0] != "p1" {
		t.Fatalf("unexpected unknown names: %v", sum.UnknownNames)
	}
}
