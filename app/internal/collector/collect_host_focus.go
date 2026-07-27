package collector

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// HostFocusReport combines alert history and detailed metrics from a single host resolution
// and a single event.get call.
type HostFocusReport struct {
	AlertHistory HostAlertHistory   `json:"alert_history"`
	Detailed     HostDetailedReport `json:"detailed"`
}

// CollectHostFocusReport resolves the host once, fetches trigger events once, and builds
// both the alert timeline and metric charts.
func CollectHostFocusReport(apiUrl, token, hostFilter string, timeFrom, timeTo int64, req ApiRequester, opts HostFocusOptions) (HostFocusReport, error) {
	var out HostFocusReport
	if hostFilter == "" {
		return out, fmt.Errorf("host filter is required")
	}
	if timeFrom >= timeTo {
		return out, fmt.Errorf("timeFrom must be < timeTo")
	}

	hosts, err := CollectHosts(apiUrl, token, req)
	if err != nil {
		return out, err
	}
	resolved, err := ResolveHostByFilter(hosts, hostFilter)
	if err != nil {
		return out, err
	}

	events, err := fetchHostTriggerEvents(apiUrl, token, resolved.HostID, timeFrom, timeTo, req)
	if err != nil {
		return out, err
	}

	out.AlertHistory = buildHostAlertHistory(resolved, events, timeFrom, timeTo)
	detailed, err := buildHostDetailedReport(resolved, events, apiUrl, token, timeFrom, timeTo, req, opts)
	if err != nil {
		return out, err
	}
	out.Detailed = detailed
	return out, nil
}

func fetchHostTriggerEvents(apiUrl, token, hostID string, timeFrom, timeTo int64, req ApiRequester) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output":              "extend",
		"hostids":             hostID,
		"source":              0,
		"object":              0,
		"time_from":           timeFrom,
		"time_till":           timeTo,
		"sortfield":           "clock",
		"sortorder":           "ASC",
		"selectRelatedObject": "extend",
		"limit":               eventFetchLimit,
	}
	return CollectRawList(apiUrl, token, "event.get", params, req)
}

func buildHostAlertHistory(resolved ResolvedHost, events []map[string]interface{}, timeFrom, timeTo int64) HostAlertHistory {
	history := HostAlertHistory{
		HostID: resolved.HostID,
		Name:   resolved.Name,
	}

	bucketSize := int64(24 * 60 * 60 * 7)
	if timeTo-timeFrom > 90*24*60*60 {
		bucketSize = 24 * 60 * 60 * 14
	}
	if timeTo-timeFrom <= 14*24*60*60 {
		bucketSize = 24 * 60 * 60 * 1
	}

	buckets := map[string]*HostAlertHistoryPoint{}
	for _, ev := range events {
		clk := parseInt(fmtValue(ev["clock"]))
		if clk < timeFrom || clk > timeTo {
			continue
		}
		history.TotalEvents++
		bucketStart := (clk - timeFrom) / bucketSize
		bucketKey := fmt.Sprintf("%d", bucketStart)
		point, ok := buckets[bucketKey]
		if !ok {
			point = &HostAlertHistoryPoint{Label: formatBucketLabel(timeFrom, bucketStart, bucketSize)}
			buckets[bucketKey] = point
		}
		val := fmtValue(ev["value"])
		if val == "1" || val == "PROBLEM" {
			point.Problems++
			history.TotalProblems++
		} else if val == "0" || val == "OK" {
			point.Resolved++
			history.TotalResolved++
		}
	}

	keys := sortedKeys(buckets)
	history.Data = make([]HostAlertHistoryPoint, 0, len(keys))
	for _, k := range keys {
		history.Data = append(history.Data, *buckets[k])
	}
	return history
}

func buildHostDetailedReport(resolved ResolvedHost, events []map[string]interface{}, apiUrl, token string, timeFrom, timeTo int64, req ApiRequester, opts HostFocusOptions) (HostDetailedReport, error) {
	var out HostDetailedReport
	hostID := resolved.HostID
	hostName := resolved.Name

	out.Host = map[string]string{"hostid": hostID, "name": hostName}
	out.Period = map[string]int64{"from": timeFrom, "to": timeTo}
	out.EventsTruncated = len(events) >= eventFetchLimit

	alerts := make([]AlertEvent, 0, len(events))
	for _, e := range events {
		name := fmtValue(e["name"])
		if name == "" {
			if ro, ok := e["relatedObject"].(map[string]interface{}); ok {
				name = fmtValue(ro["description"])
				if name == "" {
					name = fmtValue(ro["name"])
				}
			}
		}
		alertType := classifyAlertType(name)
		alerts = append(alerts, AlertEvent{
			EventID:   fmtValue(e["eventid"]),
			TriggerID: fmtValue(e["objectid"]),
			Clock:     parseInt(fmtValue(e["clock"])),
			Value:     fmtValue(e["value"]),
			Name:      name,
			Type:      alertType,
		})
	}
	enrichAlertDurations(alerts, timeTo)
	out.Alerts = alerts
	out.AlertTypeCounts = computeAlertTypeCounts(alerts)
	if len(alerts) == 0 {
		out.Status = "no_events"
	} else {
		out.Status = "ok"
	}

	items, err := CollectItemsByHost(apiUrl, token, hostID, req)
	if err != nil {
		return out, err
	}

	candidates := map[string][]map[string]interface{}{
		"cpu":    {},
		"memory": {},
		"net":    {},
	}
	for _, it := range items {
		key := strings.ToLower(fmtValue(it["key_"]))
		name := strings.ToLower(fmtValue(it["name"]))
		if strings.Contains(key, "cpu") || strings.Contains(name, "cpu") {
			candidates["cpu"] = append(candidates["cpu"], it)
			continue
		}
		if strings.Contains(key, "mem") || strings.Contains(name, "mem") || strings.Contains(key, "memory") {
			candidates["memory"] = append(candidates["memory"], it)
			continue
		}
		if strings.Contains(key, "net.if") || strings.Contains(key, "net") || strings.Contains(name, "eth") || strings.Contains(name, "network") {
			candidates["net"] = append(candidates["net"], it)
			continue
		}
	}

	metricItems := selectMetricItems(candidates, opts.MetricItemKeys)

	metricsOut := map[string]MetricSeries{}
	span := timeTo - timeFrom
	step := int64(math.Max(60, float64(span/300)))
	if step < 60 {
		step = 60
	}

	warnings := []string{}
	for name, item := range metricItems {
		itemid := fmtValue(item["itemid"])
		if itemid == "" {
			continue
		}
		days := float64(span) / 86400.0
		var pts []map[string]interface{}
		var fetchErr error
		if days > 7 {
			params := map[string]interface{}{"output": "extend", "itemids": itemid, "time_from": timeFrom, "time_till": timeTo}
			pts, fetchErr = CollectRawList(apiUrl, token, "trend.get", params, req)
		} else {
			vt := fmtValue(item["value_type"])
			history := 0
			if vt == "3" {
				history = 3
			}
			params := map[string]interface{}{"output": "extend", "history": history, "itemids": itemid, "time_from": timeFrom, "time_till": timeTo, "sortfield": "clock", "sortorder": "ASC", "limit": 100000}
			pts, fetchErr = CollectRawList(apiUrl, token, "history.get", params, req)
		}
		if fetchErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", name, fetchErr))
			continue
		}
		normalized := make([]map[string]interface{}, 0, len(pts))
		for _, p := range pts {
			clk := parseInt(fmtValue(p["clock"]))
			val := p["value"]
			if val == nil {
				if n, ok := p["num"]; ok {
					val = n
				}
			}
			normalized = append(normalized, map[string]interface{}{"clock": clk, "value": fmtValue(val)})
		}
		timestamps, values := resampleSeries(normalized, timeFrom, timeTo, step)
		metricsOut[name] = MetricSeries{
			ItemID:     itemid,
			ItemKey:    fmtValue(item["key_"]),
			ItemName:   fmtValue(item["name"]),
			Timestamps: timestamps,
			Values:     values,
		}
	}

	if out.EventsTruncated {
		warnings = append(warnings, "events truncated at 10000")
	}
	if out.Status == "no_events" {
		warnings = append(warnings, "no events in period")
	}
	for _, key := range []string{"cpu", "memory", "net"} {
		if len(candidates[key]) == 0 {
			warnings = append(warnings, "no "+key+" item found")
		}
	}
	out.Warnings = warnings
	out.Metrics = metricsOut
	return out, nil
}

func selectMetricItems(candidates map[string][]map[string]interface{}, overrides map[string]string) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	for category, arr := range candidates {
		if len(arr) == 0 {
			continue
		}
		pick := arr[0]
		if overrides != nil {
			if needle := strings.TrimSpace(strings.ToLower(overrides[category])); needle != "" {
				for _, it := range arr {
					key := strings.ToLower(fmtValue(it["key_"]))
					name := strings.ToLower(fmtValue(it["name"]))
					id := fmtValue(it["itemid"])
					if id == needle || key == needle || strings.Contains(key, needle) || strings.Contains(name, needle) {
						pick = it
						break
					}
				}
			}
		}
		out[category] = pick
	}
	return out
}

func enrichAlertDurations(alerts []AlertEvent, periodEnd int64) {
	type pending struct {
		eventID string
		clock   int64
	}
	open := map[string]pending{}
	durations := map[string]int64{}

	sorted := make([]AlertEvent, len(alerts))
	copy(sorted, alerts)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Clock == sorted[j].Clock {
			return sorted[i].EventID < sorted[j].EventID
		}
		return sorted[i].Clock < sorted[j].Clock
	})

	for _, ev := range sorted {
		tid := ev.TriggerID
		if tid == "" {
			continue
		}
		if isProblemEvent(ev.Value) {
			open[tid] = pending{eventID: ev.EventID, clock: ev.Clock}
			continue
		}
		if p, ok := open[tid]; ok {
			durations[p.eventID] = ev.Clock - p.clock
			delete(open, tid)
		}
	}
	for _, p := range open {
		durations[p.eventID] = periodEnd - p.clock
	}
	for i := range alerts {
		if d, ok := durations[alerts[i].EventID]; ok && isProblemEvent(alerts[i].Value) {
			sec := d
			alerts[i].DurationSec = &sec
		}
	}
}

func formatBucketLabel(timeFrom int64, idx int64, bucketSize int64) string {
	start := time.Unix(timeFrom+idx*bucketSize, 0)
	if bucketSize >= 24*60*60*7 {
		return start.Format("02 Jan")
	}
	return start.Format("02 Jan 15:04")
}

func parseInt(v string) int64 {
	if v == "" {
		return 0
	}
	var out int64
	_, _ = fmt.Sscanf(v, "%d", &out)
	return out
}

func sortedKeys(m map[string]*HostAlertHistoryPoint) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
