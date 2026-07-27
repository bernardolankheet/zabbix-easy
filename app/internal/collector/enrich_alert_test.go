package collector

import "testing"

func TestEnrichAlertDurations(t *testing.T) {
	alerts := []AlertEvent{
		{EventID: "p1", TriggerID: "100", Clock: 1000, Value: "1", Name: "Problem"},
		{EventID: "r1", TriggerID: "100", Clock: 1600, Value: "0", Name: "Resolved"},
		{EventID: "p2", TriggerID: "200", Clock: 2000, Value: "1", Name: "Open"},
	}
	enrichAlertDurations(alerts, 5000)
	if alerts[0].DurationSec == nil || *alerts[0].DurationSec != 600 {
		t.Fatalf("expected 600s duration for p1, got %v", alerts[0].DurationSec)
	}
	if alerts[2].DurationSec == nil || *alerts[2].DurationSec != 3000 {
		t.Fatalf("expected open problem duration 3000s, got %v", alerts[2].DurationSec)
	}
}
