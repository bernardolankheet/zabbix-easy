package report

import (
	"fmt"
	htmlpkg "html"

	"go-zabbix-report/internal/collector"
)

// HostCompareData holds two host focus reports for side-by-side comparison.
type HostCompareData struct {
	HostA MetaAndFocus
	HostB MetaAndFocus
	Days  int
}

// MetaAndFocus pairs display meta with collected focus data.
type MetaAndFocus struct {
	Meta               HostMeta
	AlertHistory       collector.HostAlertHistory
	AlertHistoryStatus string
	Detailed           collector.HostDetailedReport
	DetailedStatus     string
}

// BuildHostCompareTabHTML renders a comparison tab when two hosts are selected.
func BuildHostCompareTabHTML(data HostCompareData) string {
	if data.HostA.Meta.NameEsc == "" || data.HostB.Meta.NameEsc == "" {
		return ""
	}

	html := `<div id='tab-host-compare' class='tab-panel' style='display:none;'>`
	html += `<h2 class='tab-print-title' data-i18n='tabs.host_compare'></h2>`
	html += `<section class='host-compare-panel'>`
	html += `<p class='host-focus-panel__section-caption' data-i18n='host_compare.caption'></p>`
	html += `<div class='host-compare-grid'>`
	html += buildHostCompareColumn("A", data.HostA, data.Days)
	html += buildHostCompareColumn("B", data.HostB, data.Days)
	html += `</div></section></div>`
	return html
}

func buildHostCompareColumn(label string, host MetaAndFocus, days int) string {
	counts := host.Detailed.AlertTypeCounts
	html := `<div class='host-compare-column'>`
	html += `<div class='host-compare-column__head'><span class='host-compare-label'>Host ` + label + `</span><h3>` + host.Meta.NameEsc + `</h3></div>`
	html += `<div class='host-focus-panel__stats host-focus-panel__stats--types'>`
	html += `<div class='host-focus-stat'><span class='host-focus-stat__value'>` + formatInt(host.AlertHistory.TotalProblems) + `</span><span class='host-focus-stat__label' data-i18n='host_focus.problems'></span></div>`
	html += `<div class='host-focus-stat host-focus-stat--drop'><span class='host-focus-stat__value'>` + formatInt(counts.HostDrops) + `</span><span class='host-focus-stat__label' data-i18n='host_focus.host_drops'></span></div>`
	html += `<div class='host-focus-stat host-focus-stat--loss'><span class='host-focus-stat__value'>` + formatInt(counts.PacketLoss) + `</span><span class='host-focus-stat__label' data-i18n='host_focus.packet_loss'></span></div>`
	html += `<div class='host-focus-stat host-focus-stat--failure'><span class='host-focus-stat__value'>` + formatInt(counts.EquipmentFailures) + `</span><span class='host-focus-stat__label' data-i18n='host_focus.equipment_failures'></span></div>`
	html += `<div class='host-focus-stat host-focus-stat--other'><span class='host-focus-stat__value'>` + formatInt(counts.Others) + `</span><span class='host-focus-stat__label' data-i18n='host_focus.others'></span></div>`
	html += `</div>`
	if host.DetailedStatus != "" {
		html += `<div class='host-focus-panel__error'>` + htmlpkg.EscapeString(host.DetailedStatus) + `</div>`
	}
	html += `<div class='host-compare-period'><strong data-i18n='host_focus.period'></strong> ` + fmt.Sprintf("%d", days) + ` <span data-i18n='host_focus.days_suffix'></span></div>`
	html += `</div>`
	return html
}
