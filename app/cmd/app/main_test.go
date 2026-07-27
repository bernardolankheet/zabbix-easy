package main

import (
	"testing"

	"go-zabbix-report/internal/collector"
)

func TestBuildHostMeta(t *testing.T) {
	meta := buildHostMeta("filter-name", collector.HostAlertHistory{
		HostID: "42",
		Name:   "Resolved Name",
	}, collector.HostDetailedReport{
		Host: map[string]string{"hostid": "42", "name": "Resolved Name"},
	}, 90)
	if meta.NameEsc != "Resolved Name" {
		t.Fatalf("NameEsc = %q", meta.NameEsc)
	}
	if meta.HostIDEsc != "42" {
		t.Fatalf("HostIDEsc = %q", meta.HostIDEsc)
	}
	if meta.Days != 90 {
		t.Fatalf("Days = %d", meta.Days)
	}
}
