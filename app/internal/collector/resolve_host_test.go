package collector

import "testing"

func TestResolveHostByFilter_ExactHostID(t *testing.T) {
	hosts := []map[string]interface{}{
		{"hostid": "1001", "host": "server1", "name": "Server One"},
	}
	got, err := ResolveHostByFilter(hosts, "1001")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != "1001" || got.Name != "server1" {
		t.Fatalf("unexpected match: %+v", got)
	}
}

func TestResolveHostByFilter_ExactName(t *testing.T) {
	hosts := []map[string]interface{}{
		{"hostid": "1001", "host": "server1", "name": "Server One"},
	}
	got, err := ResolveHostByFilter(hosts, "Server One")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != "1001" {
		t.Fatalf("unexpected hostid: %s", got.HostID)
	}
}

func TestResolveHostByFilter_SinglePartial(t *testing.T) {
	hosts := []map[string]interface{}{
		{"hostid": "1001", "host": "web01", "name": "Web Front"},
		{"hostid": "1002", "host": "db01", "name": "Database"},
	}
	got, err := ResolveHostByFilter(hosts, "web")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != "1001" {
		t.Fatalf("unexpected hostid: %s", got.HostID)
	}
}

func TestResolveHostByFilter_AmbiguousPartial(t *testing.T) {
	hosts := []map[string]interface{}{
		{"hostid": "1001", "host": "web01", "name": "Web 01"},
		{"hostid": "1002", "host": "web02", "name": "Web 02"},
	}
	_, err := ResolveHostByFilter(hosts, "web")
	if err == nil {
		t.Fatal("expected ambiguous match error")
	}
}

func TestResolveHostByFilter_NotFound(t *testing.T) {
	hosts := []map[string]interface{}{
		{"hostid": "1001", "host": "server1", "name": "Server One"},
	}
	_, err := ResolveHostByFilter(hosts, "missing")
	if err == nil {
		t.Fatal("expected not found error")
	}
}
