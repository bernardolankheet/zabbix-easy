package report

import (
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"sort"
	"time"

	"go-zabbix-report/internal/collector"
)

// HostMeta holds escaped display values for the host analysis tab.
type HostMeta struct {
	FilterEsc string
	NameEsc   string
	HostIDEsc string
	Days      int
	NameDiff  bool
}

// HostTabData is the input for rendering the host analysis tab.
type HostTabData struct {
	Meta               HostMeta
	AlertHistory       collector.HostAlertHistory
	AlertHistoryStatus string
	Detailed           collector.HostDetailedReport
	DetailedStatus     string
}

// BuildHostTabHTML renders the host analysis tab panel HTML.
func BuildHostTabHTML(data HostTabData) string {
	if data.Meta.FilterEsc == "" && data.Meta.NameEsc == "" {
		return ""
	}

	html := `<div id='tab-host' class='tab-panel' style='display:none;' data-auto-open='1'>`
	html += `<h2 class='tab-print-title' data-i18n='tabs.host'></h2>`
	html += `<section class='host-focus-panel' id='host-focus-panel'>`
	html += `<div class='host-focus-panel__header'>`
	html += `<div><span class='host-focus-panel__eyebrow' data-i18n='host_focus.eyebrow'></span><h3 class='host-focus-panel__title'>` + data.Meta.NameEsc + `</h3></div>`
	html += `<div class='host-focus-panel__badges'>`
	if data.Meta.HostIDEsc != "" {
		html += `<span class='host-focus-badge'><strong>ID</strong> ` + data.Meta.HostIDEsc + `</span>`
	}
	html += `<span class='host-focus-badge'><strong data-i18n='host_focus.period'></strong> ` + fmt.Sprintf("%d", data.Meta.Days) + ` <span data-i18n='host_focus.days_suffix'></span></span>`
	if data.Meta.NameDiff {
		html += `<span class='host-focus-badge'><strong data-i18n='host_focus.filter'></strong> ` + data.Meta.FilterEsc + `</span>`
	}
	html += `</div></div>`

	html += `<div class='host-focus-panel__stats'>`
	html += `<div class='host-focus-stat'><span class='host-focus-stat__value'>` + formatInt(data.AlertHistory.TotalProblems) + `</span><span class='host-focus-stat__label' data-i18n='host_focus.problems'></span></div>`
	html += `<div class='host-focus-stat'><span class='host-focus-stat__value'>` + formatInt(data.AlertHistory.TotalResolved) + `</span><span class='host-focus-stat__label' data-i18n='host_focus.resolved'></span></div>`
	html += `<div class='host-focus-stat'><span class='host-focus-stat__value'>` + formatInt(data.AlertHistory.TotalEvents) + `</span><span class='host-focus-stat__label' data-i18n='host_focus.total_events'></span></div>`
	html += `</div>`

	if data.AlertHistoryStatus != "" {
		html += `<div class='host-focus-panel__error'><strong data-i18n='host_focus.alert_error'></strong> ` + htmlpkg.EscapeString(data.AlertHistoryStatus) + `</div>`
	}
	if data.DetailedStatus != "" {
		html += `<div class='host-focus-panel__error'><strong data-i18n='host_focus.metrics_error'></strong> ` + htmlpkg.EscapeString(data.DetailedStatus) + `</div>`
	}
	for _, w := range data.Detailed.Warnings {
		if w == "no events in period" {
			continue
		}
		if w == "events truncated at 10000" {
			html += `<div class='host-focus-panel__warning host-focus-panel__warning--truncated' data-i18n='host_focus.events_truncated'></div>`
			continue
		}
		html += `<div class='host-focus-panel__warning'><span data-i18n='host_focus.warning_prefix'></span> ` + htmlpkg.EscapeString(w) + `</div>`
	}

	hostHistoryLabels := []string{}
	hostHistoryProblems := []int{}
	hostHistoryResolved := []int{}
	for _, p := range data.AlertHistory.Data {
		hostHistoryLabels = append(hostHistoryLabels, p.Label)
		hostHistoryProblems = append(hostHistoryProblems, p.Problems)
		hostHistoryResolved = append(hostHistoryResolved, p.Resolved)
	}
	hostLabelsJSON, _ := json.Marshal(hostHistoryLabels)
	hostProblemsJSON, _ := json.Marshal(hostHistoryProblems)
	hostResolvedJSON, _ := json.Marshal(hostHistoryResolved)

	html += buildAlertHistorySection(data, hostLabelsJSON, hostProblemsJSON, hostResolvedJSON)

	if data.DetailedStatus == "" {
		if len(data.Detailed.Metrics) > 0 {
			html += buildMetricsSection(data)
		} else {
			html += `<div class='host-focus-panel__section host-focus-panel__section--metrics'>`
			html += `<h4 class='host-focus-panel__section-title' data-i18n='host_focus.metrics_title'></h4>`
			html += `<div class='host-metrics-empty' data-i18n='host_focus.no_metrics'></div>`
			html += `</div>`
		}

		html += buildAlertTypeStatsSection(data)
		html += buildAlertsTableSection(data)
	}

	html += `</section></div>`
	return html
}

func buildAlertHistorySection(data HostTabData, hostLabelsJSON, hostProblemsJSON, hostResolvedJSON []byte) string {
	html := `<div class='host-focus-panel__section host-focus-panel__section--alerts'>`
	html += `<h4 class='host-focus-panel__section-title' data-i18n='host_focus.alert_history'></h4>`
	html += `<p class='host-focus-panel__section-caption' data-i18n='host_focus.alert_caption'></p>`
	if data.AlertHistoryStatus == "" && len(data.AlertHistory.Data) == 0 {
		html += `<div class='host-alert-history-empty' data-i18n='host_focus.no_events'></div>`
	} else if data.AlertHistoryStatus == "" {
		html += `<div class='host-chart-container host-chart-container--alerts'>`
		html += `<canvas id='host-alert-history-chart' class='host-alert-history-chart'></canvas>`
		html += `</div>`
		html += queueAlertChartScript(hostLabelsJSON, hostProblemsJSON, hostResolvedJSON)
	}
	html += `</div>`
	return html
}

func buildAlertTypeStatsSection(data HostTabData) string {
	counts := data.Detailed.AlertTypeCounts
	html := `<div class='host-focus-panel__section host-focus-panel__section--type-stats'>`
	html += `<h4 class='host-focus-panel__section-title' data-i18n='host_focus.type_stats_title'></h4>`
	html += `<p class='host-focus-panel__section-caption' data-i18n='host_focus.type_stats_filter_hint'></p>`
	html += `<div class='host-focus-panel__stats host-focus-panel__stats--types host-alert-type-filters'>`
	html += buildClickableStat("host_drop", counts.HostDrops, "host_focus.host_drops", "host-focus-stat--drop")
	html += buildClickableStat("packet_loss", counts.PacketLoss, "host_focus.packet_loss", "host-focus-stat--loss")
	html += buildClickableStat("equipment_failure", counts.EquipmentFailures, "host_focus.equipment_failures", "host-focus-stat--failure")
	html += buildClickableStat("other", counts.Others, "host_focus.others", "host-focus-stat--other")
	html += `</div></div>`
	return html
}

func buildClickableStat(filterType string, value int, labelKey, className string) string {
	return `<button type='button' class='host-focus-stat host-focus-stat--clickable ` + className + `' data-alert-filter='` + filterType + `'>` +
		`<span class='host-focus-stat__value'>` + formatInt(value) + `</span>` +
		`<span class='host-focus-stat__label' data-i18n='` + labelKey + `'></span></button>`
}

func buildAlertsTableSection(data HostTabData) string {
	problemAlerts := filterProblemAlerts(data.Detailed.Alerts)
	html := `<div class='host-focus-panel__section host-focus-panel__section--alert-table'>`
	html += `<h4 class='host-focus-panel__section-title' data-i18n='host_focus.alert_table_title'></h4>`
	html += `<p class='host-focus-panel__section-caption' data-i18n='host_focus.alert_table_caption'></p>`

	if len(problemAlerts) == 0 {
		html += `<div class='host-alert-history-empty' data-i18n='host_focus.no_problem_events'></div>`
		html += `</div>`
		return html
	}

	html += `<div class='host-alerts-table-toolbar'>`
	html += `<button type='button' class='host-alert-filter-clear db-btn db-btn-ghost db-btn--compact' data-alert-filter='all' data-i18n='host_focus.filter_all'></button>`
	html += `<select id='host-alert-type-select' class='host-alert-type-select' aria-label='Filter alert type'>`
	html += `<option value='all' data-i18n='host_focus.filter_all'></option>`
	html += `<option value='host_drop' data-i18n='host_focus.alert_type.host_drop'></option>`
	html += `<option value='packet_loss' data-i18n='host_focus.alert_type.packet_loss'></option>`
	html += `<option value='equipment_failure' data-i18n='host_focus.alert_type.equipment_failure'></option>`
	html += `<option value='other' data-i18n='host_focus.alert_type.other'></option>`
	html += `</select>`
	html += `<button type='button' class='db-btn db-btn-primary' id='host-alerts-export-csv' data-i18n='host_focus.export_csv'></button>`
	html += `</div>`

	html += `<div class='table-responsive host-alerts-table-wrap'>`
	html += `<table class='modern-table host-alerts-table' id='host-alerts-table'>`
	html += `<thead><tr>`
	html += `<th data-i18n='host_focus.alert_datetime'></th>`
	html += `<th data-i18n='host_focus.alert_duration'></th>`
	html += `<th data-i18n='host_focus.alert_type'></th>`
	html += `<th data-i18n='host_focus.alert_name'></th>`
	html += `</tr></thead><tbody>`

	for _, alert := range problemAlerts {
		html += `<tr data-alert-type='` + string(alert.Type) + `'>`
		html += `<td>` + htmlpkg.EscapeString(formatAlertDateTime(alert.Clock)) + `</td>`
		html += `<td>` + htmlpkg.EscapeString(formatAlertDuration(alert.DurationSec)) + `</td>`
		html += `<td><span class='host-alert-type host-alert-type--` + string(alert.Type) + `' data-i18n='` + alertTypeI18nKey(alert.Type) + `'></span></td>`
		html += `<td>` + htmlpkg.EscapeString(alert.Name) + `</td>`
		html += `</tr>`
	}

	html += `</tbody></table></div></div>`
	return html
}

func filterProblemAlerts(alerts []collector.AlertEvent) []collector.AlertEvent {
	out := make([]collector.AlertEvent, 0, len(alerts))
	for _, a := range alerts {
		if a.Value == "1" || a.Value == "PROBLEM" {
			out = append(out, a)
		}
	}
	sortAlertsDesc(out)
	return out
}

func sortAlertsDesc(alerts []collector.AlertEvent) {
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].Clock > alerts[j].Clock
	})
}

func alertTypeI18nKey(t collector.AlertType) string {
	switch t {
	case collector.AlertTypeHostDrop:
		return "host_focus.alert_type.host_drop"
	case collector.AlertTypePacketLoss:
		return "host_focus.alert_type.packet_loss"
	case collector.AlertTypeEquipmentFailure:
		return "host_focus.alert_type.equipment_failure"
	default:
		return "host_focus.alert_type.other"
	}
}

func formatAlertDateTime(clock int64) string {
	return time.Unix(clock, 0).Format("02/01/2006 15:04:05")
}

func formatAlertDuration(durationSec *int64) string {
	if durationSec == nil {
		return "—"
	}
	sec := *durationSec
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%dm %ds", sec/60, sec%60)
	}
	if sec < 86400 {
		return fmt.Sprintf("%dh %dm", sec/3600, (sec%3600)/60)
	}
	return fmt.Sprintf("%dd %dh", sec/86400, (sec%86400)/3600)
}

func buildMetricsSection(data HostTabData) string {
	html := `<div class='host-focus-panel__section host-focus-panel__section--metrics'>`
	html += `<div class='host-focus-panel__section-head'><h4 class='host-focus-panel__section-title' data-i18n='host_focus.metrics_title'></h4>`
	html += `<button class='db-btn db-btn-primary' onclick="window.zbx_downloadAllCharts && window.zbx_downloadAllCharts('host-metric-')" data-i18n='host_focus.export_all'></button></div>`
	html += `<div class='host-focus-panel__metrics-grid'>`

	alertsJSON, _ := json.Marshal(data.Detailed.Alerts)
	metricOrder := []string{"cpu", "memory", "net"}
	for _, mname := range metricOrder {
		ms, ok := data.Detailed.Metrics[mname]
		if !ok {
			continue
		}
		labels := make([]string, 0, len(ms.Timestamps))
		timestamps := make([]int64, 0, len(ms.Timestamps))
		for _, ts := range ms.Timestamps {
			labels = append(labels, formatMetricChartLabel(ts, data.Meta.Days))
			timestamps = append(timestamps, ts)
		}
		vals := make([]interface{}, 0, len(ms.Values))
		for _, vp := range ms.Values {
			if vp == nil {
				vals = append(vals, nil)
			} else {
				vals = append(vals, *vp)
			}
		}
		mLabelsJSON, _ := json.Marshal(labels)
		mValuesJSON, _ := json.Marshal(vals)
		mTimestampsJSON, _ := json.Marshal(timestamps)
		cid := "host-metric-" + mname + "-chart"
		html += `<div class='host-metric-card host-metric-card--` + mname + `'>` 
		html += `<div class='host-metric-card__head'><span class='host-metric-card__dot host-metric-card__dot--` + mname + `' aria-hidden='true'></span>`
		html += `<h4 data-i18n='chart.metric.` + mname + `'></h4></div>`
		if ms.ItemName != "" || ms.ItemKey != "" {
			html += `<p class='host-metric-item-legend' title='` + htmlpkg.EscapeString(ms.ItemKey) + `'>` + htmlpkg.EscapeString(ms.ItemName)
			if ms.ItemKey != "" {
				html += ` <code class='host-metric-item-key'>` + htmlpkg.EscapeString(ms.ItemKey) + `</code>`
			}
			html += `</p>`
		}
		html += `<div class='host-chart-container host-chart-container--metric'>`
		html += `<canvas id='` + cid + `' class='host-metric-chart'></canvas>`
		html += `</div>`
		html += `<div class='host-metric-meta'><span class='host-metric-meta__count'>` + fmt.Sprintf("%d", len(data.Detailed.Alerts)) + ` <span data-i18n='host_focus.alerts_on_chart'></span></span>`
		html += `<button type='button' class='db-btn db-btn-primary db-btn--compact' onclick="window.zbx_downloadChart && window.zbx_downloadChart('` + cid + `','` + cid + `.png')" data-i18n='host_focus.export_png'></button></div>`
		html += queueMetricChartScript(cid, mname, mLabelsJSON, mValuesJSON, mTimestampsJSON, alertsJSON)
		html += `</div>`
	}
	html += `</div></div>`
	return html
}

func queueAlertChartScript(labels, problems, resolved []byte) string {
	return `<script>if(window.zbxQueueHostChart){window.zbxQueueHostChart.push({type:"alert",id:"host-alert-history-chart",labels:` + string(labels) + `,problems:` + string(problems) + `,resolved:` + string(resolved) + `});}</script>`
}

func queueMetricChartScript(cid, mname string, labels, values, timestamps, alerts []byte) string {
	return `<script>if(window.zbxQueueHostChart){window.zbxQueueHostChart.push({type:"metric",id:"` + cid + `",metric:"` + mname + `",labels:` + string(labels) + `,values:` + string(values) + `,timestamps:` + string(timestamps) + `,alerts:` + string(alerts) + `});}</script>`
}

func formatMetricChartLabel(ts int64, periodDays int) string {
	if periodDays > 30 {
		return time.Unix(ts, 0).Format("02/01/06")
	}
	if periodDays > 7 {
		return time.Unix(ts, 0).Format("02 Jan")
	}
	return time.Unix(ts, 0).Format("02 Jan 15:04")
}

func formatInt(n int) string {
	return fmt.Sprintf("%d", n)
}
