package report

import (
	"strings"
	"testing"

	"go-zabbix-report/internal/collector"
)

func TestBuildHostTabHTML_IncludesFiltersAndDuration(t *testing.T) {
	sec := int64(3600)
	html := BuildHostTabHTML(HostTabData{
		Meta: HostMeta{NameEsc: "Host A", Days: 30},
		Detailed: collector.HostDetailedReport{
			Status: "ok",
			Alerts: []collector.AlertEvent{
				{EventID: "1", Value: "1", Clock: 1700000000, Name: "SNMP Indisponível", Type: collector.AlertTypeHostDrop, DurationSec: &sec},
			},
			AlertTypeCounts: collector.HostAlertTypeCounts{HostDrops: 1},
			Metrics: map[string]collector.MetricSeries{
				"cpu": {ItemID: "10", ItemKey: "system.cpu.util", ItemName: "CPU utilization"},
			},
		},
	})
	if !strings.Contains(html, "host-alert-type-filters") {
		t.Fatal("expected clickable KPI filters")
	}
	if !strings.Contains(html, "host-alerts-export-csv") {
		t.Fatal("expected CSV export button")
	}
	if !strings.Contains(html, "host_focus.alert_duration") {
		t.Fatal("expected duration column header")
	}
	if !strings.Contains(html, "host-metric-item-legend") {
		t.Fatal("expected metric item legend")
	}
}

func TestBuildHostCompareTabHTML(t *testing.T) {
	html := BuildHostCompareTabHTML(HostCompareData{
		Days: 30,
		HostA: MetaAndFocus{Meta: HostMeta{NameEsc: "Host A"}},
		HostB: MetaAndFocus{Meta: HostMeta{NameEsc: "Host B"}},
	})
	if !strings.Contains(html, "tab-host-compare") {
		t.Fatal("expected compare tab")
	}
	if !strings.Contains(html, "Host A") || !strings.Contains(html, "Host B") {
		t.Fatal("expected both host names")
	}
}

func TestFormatAlertDuration(t *testing.T) {
	if formatAlertDuration(nil) != "—" {
		t.Fatal("nil duration should render dash")
	}
	sec := int64(125)
	got := formatAlertDuration(&sec)
	if got != "2m 5s" {
		t.Fatalf("expected 2m 5s, got %q", got)
	}
}
