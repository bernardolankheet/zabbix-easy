package collector

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// AlertType categorizes host trigger events for KPIs and the alerts table.
type AlertType string

const (
	AlertTypeHostDrop         AlertType = "host_drop"
	AlertTypePacketLoss       AlertType = "packet_loss"
	AlertTypeEquipmentFailure AlertType = "equipment_failure"
	AlertTypeOther            AlertType = "other"
)

const eventFetchLimit = 10000

type alertRuleSet struct {
	HostDrop           []string `json:"host_drop"`
	PacketLoss         []string `json:"packet_loss"`
	EquipmentFailure   []string `json:"equipment_failure"`
}

var (
	rulesOnce sync.Once
	rules     alertRuleSet
)

func defaultAlertRules() alertRuleSet {
	return alertRuleSet{
		HostDrop: []string{
			"snmp indisponível", "snmp indisponivel", "snmp unavailable",
			"icmp ping failed", "unreachable", "indisponível", "indisponivel", "queda",
			"host down", "is down", "not available", "não disponível", "nao disponivel",
			"agent ping", "icmp ping", "icmp unavailable", "no data",
			"connection refused", "connection timed out", "unavailable",
			"not responding", "sem resposta", "inacessível", "inacessivel",
			"ping loss", "sem conectividade", "without connectivity",
		},
		PacketLoss: []string{
			"packet loss", "perda de pacote", "perdas de pacote", "perda de pacotes",
			"icmp loss", "loss %", "packet dropped", "pacotes perdidos", "perda icmp",
		},
		EquipmentFailure: []string{
			"link down", "interface down", "port down", "port status down",
			"hardware", "fan failure", "power supply", "psu", "falha",
			"failure", "equipment", "equipamento", "temperatura alta",
			"high temperature", "overheat", "reboot", "restart detected",
			"snmp error", "sensor", "disk failure", "falha de hardware",
			"interface oper status", "operational status down",
		},
	}
}

func loadAlertRules() alertRuleSet {
	rulesOnce.Do(func() {
		rules = defaultAlertRules()
		paths := []string{
			"./web/config/alert_rules.json",
			"web/config/alert_rules.json",
			"/root/web/config/alert_rules.json",
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			var loaded alertRuleSet
			if json.Unmarshal(b, &loaded) == nil {
				if len(loaded.HostDrop) > 0 {
					rules.HostDrop = loaded.HostDrop
				}
				if len(loaded.PacketLoss) > 0 {
					rules.PacketLoss = loaded.PacketLoss
				}
				if len(loaded.EquipmentFailure) > 0 {
					rules.EquipmentFailure = loaded.EquipmentFailure
				}
			}
			break
		}
	})
	return rules
}

func classifyAlertType(name string) AlertType {
	n := strings.ToLower(name)
	r := loadAlertRules()
	for _, kw := range r.PacketLoss {
		if strings.Contains(n, kw) {
			return AlertTypePacketLoss
		}
	}
	for _, kw := range r.HostDrop {
		if strings.Contains(n, kw) {
			return AlertTypeHostDrop
		}
	}
	for _, kw := range r.EquipmentFailure {
		if strings.Contains(n, kw) {
			return AlertTypeEquipmentFailure
		}
	}
	return AlertTypeOther
}

func isProblemEvent(value string) bool {
	return value == "1" || value == "PROBLEM"
}

func computeAlertTypeCounts(alerts []AlertEvent) HostAlertTypeCounts {
	var counts HostAlertTypeCounts
	for _, a := range alerts {
		if !isProblemEvent(a.Value) {
			continue
		}
		switch a.Type {
		case AlertTypeHostDrop:
			counts.HostDrops++
		case AlertTypePacketLoss:
			counts.PacketLoss++
		case AlertTypeEquipmentFailure:
			counts.EquipmentFailures++
		default:
			counts.Others++
		}
	}
	return counts
}
