package collector

import "testing"

func TestClassifyAlertType(t *testing.T) {
	cases := []struct {
		name     string
		expected AlertType
	}{
		{"Host unreachable for 5 minutes", AlertTypeHostDrop},
		{"ICMP ping unavailable", AlertTypeHostDrop},
		{"Queda de conectividade", AlertTypeHostDrop},
		{"High ICMP packet loss (>20%)", AlertTypePacketLoss},
		{"Perda de pacotes ICMP", AlertTypePacketLoss},
		{"Interface {#IFNAME}: Link down", AlertTypeEquipmentFailure},
		{"Falha no sensor de temperatura", AlertTypeEquipmentFailure},
		{"SNMP Indisponível", AlertTypeHostDrop},
		{"CPU usage high", AlertTypeOther},
	}

	for _, tc := range cases {
		got := classifyAlertType(tc.name)
		if got != tc.expected {
			t.Errorf("classifyAlertType(%q) = %q, want %q", tc.name, got, tc.expected)
		}
	}
}

func TestComputeAlertTypeCounts(t *testing.T) {
	alerts := []AlertEvent{
		{Value: "1", Type: AlertTypeHostDrop},
		{Value: "1", Type: AlertTypeHostDrop},
		{Value: "0", Type: AlertTypeHostDrop},
		{Value: "1", Type: AlertTypePacketLoss},
		{Value: "1", Type: AlertTypeEquipmentFailure},
		{Value: "1", Type: AlertTypeOther},
	}
	counts := computeAlertTypeCounts(alerts)
	if counts.HostDrops != 2 {
		t.Fatalf("HostDrops = %d, want 2", counts.HostDrops)
	}
	if counts.PacketLoss != 1 {
		t.Fatalf("PacketLoss = %d, want 1", counts.PacketLoss)
	}
	if counts.EquipmentFailures != 1 {
		t.Fatalf("EquipmentFailures = %d, want 1", counts.EquipmentFailures)
	}
	if counts.Others != 1 {
		t.Fatalf("Others = %d, want 1", counts.Others)
	}
}
