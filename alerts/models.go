// Package alerts provides the Alerting and Notification Engine for LightHouse.
// It surfaces persistent rule definitions from SQLite and describes the
// canonical payload sent to every downstream notification channel.
package alerts

import "time"

// AlertRule mirrors a row in the alert_rules table.
// Container pattern and channel configuration are stored as raw strings so
// they can be persisted directly to/from SQLite without any extra marshalling.
type AlertRule struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	ContainerPattern string `json:"container_pattern"`
	// EventTypes is a comma-separated list of Docker daemon event actions to
	// watch.  Supported values: "die", "oom", "health_status".
	EventTypes      string `json:"event_types"`
	// LogPattern is a Go regular expression that is matched against every log
	// line produced by a targeted container (e.g. "(?i)error|exception|fatal").
	// An empty string disables log scanning for this rule.
	LogPattern      string `json:"log_pattern"`
	Enabled         bool   `json:"enabled"`
	// CooldownSeconds is the minimum number of seconds that must elapse between
	// two consecutive notifications for the same rule, preventing alert storms.
	CooldownSeconds int    `json:"cooldown_seconds"`
	EnableSlack            bool    `json:"enable_slack"`
	EnableMSTeams          bool    `json:"enable_msteams"`
	EnableGChat            bool    `json:"enable_gchat"`
	EnableGenericWebhook   bool    `json:"enable_generic_webhook"`
	EnableEmail     bool   `json:"enable_email"`
	EmailAddress    string `json:"email_address"`
	MetricCPUThreshold float64 `json:"metric_cpu_threshold"`
	MetricMemThreshold int64   `json:"metric_mem_threshold"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
}

// NotificationPayload is the normalised message that the AlertManager passes
// to the delivery layer whenever a rule fires.
type NotificationPayload struct {
	RuleName      string `json:"rule_name"`
	ContainerName string `json:"container_name"`
	// Type is either "event" (Docker lifecycle) or "log" (pattern match).
	Type      string `json:"type"`
	// Details carries the raw Docker event action or the matching log line.
	Details   string `json:"details"`
	Timestamp string `json:"timestamp"`
}
