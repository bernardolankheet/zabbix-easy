package collector

// HostAlertHistoryPoint aggregates alert events in a time bucket.
type HostAlertHistoryPoint struct {
	Label    string `json:"label"`
	Problems int    `json:"problems"`
	Resolved int    `json:"resolved"`
}

// HostAlertHistory contains the host identifier and a compact timeline of alert events.
type HostAlertHistory struct {
	HostID        string                  `json:"host_id"`
	Name          string                  `json:"name"`
	Data          []HostAlertHistoryPoint `json:"data"`
	TotalProblems int                     `json:"total_problems"`
	TotalResolved int                     `json:"total_resolved"`
	TotalEvents   int                     `json:"total_events"`
}

// HostAlertTypeCounts aggregates problem events by alert category.
type HostAlertTypeCounts struct {
	HostDrops           int `json:"host_drops"`
	PacketLoss          int `json:"packet_loss"`
	EquipmentFailures   int `json:"equipment_failures"`
	Others              int `json:"others"`
}

// HostFocusOptions configures optional host analysis behaviour.
type HostFocusOptions struct {
	MetricItemKeys map[string]string `json:"metric_item_keys"`
}

// AlertEvent is a normalized event/alert extracted from Zabbix event.get.
type AlertEvent struct {
	EventID     string    `json:"eventid"`
	TriggerID   string    `json:"triggerid"`
	Clock       int64     `json:"clock"`
	Value       string    `json:"value"`
	Name        string    `json:"name"`
	Type        AlertType `json:"type"`
	DurationSec *int64    `json:"duration_sec,omitempty"`
}

// MetricSeries represents a timeseries aligned to timestamps (unix seconds).
type MetricSeries struct {
	ItemID     string     `json:"itemid"`
	ItemKey    string     `json:"item_key"`
	ItemName   string     `json:"item_name"`
	Timestamps []int64    `json:"timestamps"`
	Values     []*float64 `json:"values"`
}

// HostDetailedReport is the consolidated report returned to callers.
type HostDetailedReport struct {
	Status          string                  `json:"status"`
	Host            map[string]string       `json:"host"`
	Period          map[string]int64        `json:"period"`
	Alerts          []AlertEvent            `json:"alerts"`
	AlertTypeCounts HostAlertTypeCounts     `json:"alert_type_counts"`
	EventsTruncated bool                    `json:"events_truncated"`
	Metrics         map[string]MetricSeries `json:"metrics"`
	Warnings        []string                `json:"warnings"`
}
