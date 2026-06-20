package alerts

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"time"
)

// ─── Slack / Discord wire types ───────────────────────────────────────────────

// slackPayload is the outer wrapper sent to a Slack (or Discord) Incoming
// Webhook.  Both platforms accept the same "attachments" envelope.
type slackPayload struct {
	Text        string            `json:"text,omitempty"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

// slackAttachment represents a single coloured card inside the message.
type slackAttachment struct {
	// Color is a hex colour string: "#36a64f" (green) for log matches,
	// "#f23d4f" (red) for lifecycle crash events.
	Color     string       `json:"color"`
	Title     string       `json:"title"`
	TitleLink string       `json:"title_link,omitempty"`
	Text      string       `json:"text,omitempty"`
	Fields    []slackField `json:"fields,omitempty"`
	Footer    string       `json:"footer,omitempty"`
	Ts        int64        `json:"ts,omitempty"` // Unix epoch; Slack renders as a date stamp
}

// slackField is a compact key/value pair rendered as a table inside the card.
type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// ─── Public API ───────────────────────────────────────────────────────────────

// DeliverNotification dispatches a NotificationPayload to the target channel
// described by channelType and the JSON blob in configJSON.
//
// Supported channelType values:
//   - "slack"            – richly formatted Slack (or Discord) Incoming Webhook
//   - "generic_webhook"  – raw JSON POST; useful for n8n, Zapier, custom sinks
//
// configJSON must be a JSON object.  Both adapters require at minimum:
//
//	{ "url": "https://hooks.slack.com/services/..." }
func DeliverNotification(channelType, configJSON string, payload NotificationPayload) error {
	var cfg map[string]string
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return fmt.Errorf("alerts/delivery: invalid channel_config JSON: %w", err)
	}

	webhookURL := cfg["url"]
	if webhookURL == "" {
		return fmt.Errorf("alerts/delivery: channel_config is missing required field \"url\"")
	}

	switch channelType {
	case "slack":
		return sendSlackWebhook(webhookURL, payload)
	case "msteams":
		return sendMSTeamsWebhook(webhookURL, payload)
	case "gchat":
		return sendGChatWebhook(webhookURL, payload)
	case "generic_webhook":
		return sendGenericWebhook(webhookURL, payload)
	default:
		return fmt.Errorf("alerts/delivery: unsupported channel_type %q", channelType)
	}
}

// ─── Adapters ─────────────────────────────────────────────────────────────────

// sendSlackWebhook builds a richly formatted Slack attachment and POSTs it to
// the provided Incoming Webhook URL.  The card colour encodes the alert type:
//   - Red  (#f23d4f) → Docker lifecycle event (container crash / OOM / unhealthy)
//   - Amber (#f0a30a) → log pattern match (e.g. "ERROR", "Exception", "Fatal")
//   - Green (#36a64f) → container recovery
func sendSlackWebhook(url string, p NotificationPayload) error {
	var color, title string

	switch p.Type {
	case "recovery":
		color = "#36a64f" // green
		title = fmt.Sprintf("🟢 LightHouse Recovery: %s is back online", p.ContainerName)
	case "event":
		color = "#f23d4f" // red
		title = fmt.Sprintf("🔴 LightHouse Alert: %s Triggered!", p.RuleName)
	case "audit":
		color = "#0891B2" // blue
		title = fmt.Sprintf("🔵 LightHouse Audit: %s", p.RuleName)
	default:
		color = "#f0a30a" // amber
		title = fmt.Sprintf("🟡 LightHouse Log Match: %s", p.RuleName)
	}

	details := p.Details
	if len(details) > 800 {
		details = details[:800] + "…"
	}

	attachment := slackAttachment{
		Color: color,
		Title: title,
		Fields: []slackField{
			{Title: "*Container*", Value: fmt.Sprintf("`%s`", p.ContainerName), Short: true},
			{Title: "*Type*",      Value: fmt.Sprintf("`%s`", p.Type),          Short: true},
			{Title: "*Details*",   Value: fmt.Sprintf("```\n%s\n```", details), Short: false},
		},
		Footer: "LightHouse Alerting Engine",
		Ts:     time.Now().Unix(),
	}

	sp := slackPayload{Attachments: []slackAttachment{attachment}}
	body, err := json.Marshal(sp)
	if err != nil {
		return fmt.Errorf("alerts/delivery: failed to marshal Slack payload: %w", err)
	}
	return postJSON(url, body)
}

// sendMSTeamsWebhook builds a Microsoft Teams MessageCard and POSTs it.
func sendMSTeamsWebhook(url string, p NotificationPayload) error {
	var color, title string

	switch p.Type {
	case "recovery":
		color = "36a64f"
		title = fmt.Sprintf("Recovery: %s is back online", p.ContainerName)
	case "event":
		color = "f23d4f"
		title = fmt.Sprintf("Alert: %s Triggered!", p.RuleName)
	case "audit":
		color = "0891B2"
		title = fmt.Sprintf("Audit: %s", p.RuleName)
	default:
		color = "f0a30a"
		title = fmt.Sprintf("Log Match: %s", p.RuleName)
	}

	payload := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "https://schema.org/extensions",
		"summary":    title,
		"themeColor": color,
		"title":      "LightHouse",
		"sections": []map[string]interface{}{{
			"activityTitle": title,
			"facts": []map[string]string{
				{"name": "Rule", "value": p.RuleName},
				{"name": "Container", "value": p.ContainerName},
				{"name": "Type", "value": p.Type},
				{"name": "Details", "value": p.Details},
			},
		}},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("alerts/delivery: failed to marshal MS Teams payload: %w", err)
	}
	return postJSON(url, body)
}

// sendGChatWebhook builds a Google Chat message payload and POSTs it.
func sendGChatWebhook(url string, p NotificationPayload) error {
	var title string

	switch p.Type {
	case "recovery":
		title = fmt.Sprintf("🟢 Recovery: %s is back online", p.ContainerName)
	case "event":
		title = fmt.Sprintf("🔴 Alert: %s Triggered!", p.RuleName)
	case "audit":
		title = fmt.Sprintf("🔵 Audit: %s", p.RuleName)
	default:
		title = fmt.Sprintf("🟡 Log Match: %s", p.RuleName)
	}

	text := fmt.Sprintf("*%s*\n*Rule:* %s\n*Container:* %s\n*Type:* %s\n```\n%s\n```",
		title, p.RuleName, p.ContainerName, p.Type, p.Details)

	payload := map[string]interface{}{
		"text": text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("alerts/delivery: failed to marshal GChat payload: %w", err)
	}
	return postJSON(url, body)
}

// sendGenericWebhook serialises the NotificationPayload directly as JSON and
// POSTs it to the configured URL.  This is the lowest-common-denominator
// adapter suitable for any HTTP webhook receiver.
func sendGenericWebhook(url string, p NotificationPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("alerts/delivery: failed to marshal generic payload: %w", err)
	}
	return postJSON(url, body)
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

// httpClient is shared across all deliveries so connections are reused where
// possible.  The 10-second timeout prevents a slow or unresponsive webhook from
// blocking the calling goroutine indefinitely.
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func isAllowedWebhookURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return false
	}
	blockedHosts := []string{"169.254.169.254", "metadata.google.internal"}
	for _, b := range blockedHosts {
		if host == b {
			return false
		}
	}
	return true
}

// postJSON sends a JSON-encoded body to url via HTTP POST and returns an error
// if the response status code is outside the 2xx range.
func postJSON(url string, body []byte) error {
	if !isAllowedWebhookURL(url) {
		return fmt.Errorf("alerts/delivery: webhook URL is not allowed (SSRF protection)")
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("alerts/delivery: could not create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LightHouse-Alerts/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("alerts/delivery: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("alerts/delivery: webhook returned non-2xx status %d", resp.StatusCode)
	}
	return nil
}

// DeliverEmail sends an email via the provided SMTP configuration.
func DeliverEmail(host string, port int, user, pass, to string, p NotificationPayload) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	var auth smtp.Auth
	if user != "" && pass != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}

	from := user
	if from == "" {
		from = "lighthouse@localhost" // Fallback if no user is provided for local SMTP relays
	}

	subject := fmt.Sprintf("Subject: [LightHouse] %s Alert - %s\r\n", p.Type, p.ContainerName)
	
	htmlBody := fmt.Sprintf(`
	<html>
	<head>
		<style>
			body { font-family: 'Inter', -apple-system, sans-serif; background: #f4f5f7; padding: 20px; color: #333; }
			.card { background: #fff; padding: 20px; border-radius: 8px; border-top: 4px solid #00dc82; box-shadow: 0 4px 6px rgba(0,0,0,0.05); max-width: 600px; margin: 0 auto; }
			h2 { margin-top: 0; color: #111; }
			.meta { margin: 15px 0; padding: 15px; background: #f8f9fa; border-radius: 6px; }
			.meta p { margin: 5px 0; font-size: 14px; }
			.details { background: #1e1e2e; color: #cdd6f4; padding: 15px; border-radius: 6px; font-family: monospace; white-space: pre-wrap; font-size: 13px; }
			.footer { margin-top: 20px; text-align: center; font-size: 12px; color: #888; }
		</style>
	</head>
	<body>
		<div class="card">
			<h2>LightHouse Alert</h2>
			<div class="meta">
				<p><strong>Rule:</strong> %s</p>
				<p><strong>Container:</strong> %s</p>
				<p><strong>Type:</strong> %s</p>
			</div>
			<h4>Details</h4>
			<div class="details">%s</div>
			<div class="footer">Sent by LightHouse Monitoring</div>
		</div>
	</body>
	</html>
	`, p.RuleName, p.ContainerName, p.Type, p.Details)

	headers := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		subject +
		"MIME-version: 1.0;\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\";\r\n\r\n"

	msg := []byte(headers + htmlBody)

	// Port 465 requires Implicit TLS
	if port == 465 {
		tlsconfig := &tls.Config{
			ServerName: host,
		}
		conn, err := tls.Dial("tcp", addr, tlsconfig)
		if err != nil {
			return fmt.Errorf("alerts/delivery: TLS dial failed: %w", err)
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("alerts/delivery: SMTP client creation failed: %w", err)
		}
		defer client.Close()
		if auth != nil {
			if err = client.Auth(auth); err != nil {
				return fmt.Errorf("alerts/delivery: SMTP auth failed: %w", err)
			}
		}
		if err = client.Mail(from); err != nil {
			return err
		}
		if err = client.Rcpt(to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		_, err = w.Write(msg)
		if err != nil {
			return err
		}
		err = w.Close()
		if err != nil {
			return err
		}
		return client.Quit()
	}

	// Standard SMTP / STARTTLS for port 587, 25, etc.
	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}
