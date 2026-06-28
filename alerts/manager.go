// Package alerts contains the Alerting and Notification Engine for LightHouse.
// It maintains an in-memory rule cache backed by SQLite, subscribes to the
// Docker daemon event stream, lazily spawns per-container log-tailer goroutines,
// and dispatches notifications through the delivery adapters in delivery.go.
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"lighthouse/db"

	"github.com/moby/moby/api/types/events"

	"github.com/moby/moby/client"
)

// ─── AlertManager ────────────────────────────────────────────────────────────

// AlertManager is the central background service. It owns:
//   - an in-memory rules cache (rulesMu / rules)
//   - a rate-limit map to enforce per-rule cooldowns (ltMu / lastTriggered)
//   - a registry of live log-tailer goroutines keyed by container ID (tailsMu / activeTails)
//   - a root context whose cancellation propagates to every child goroutine
type AlertManager struct {
	cli *client.Client

	// rulesMu guards the rules map. Use RLock for reads, Lock for writes.
	rulesMu sync.RWMutex
	rules   map[int64]*AlertRule

	// ltMu guards the lastTriggered map. A plain Mutex is used because every
	// access is a short critical section (read-then-write).
	ltMu          sync.Mutex
	lastTriggered map[int64]time.Time

	// downStateMu guards the downState map
	downStateMu sync.Mutex
	downState   map[string]bool

	// tailsMu guards the activeTails map.
	tailsMu    sync.Mutex
	activeTails map[string]context.CancelFunc // containerID → cancel

	// debounceMu guards debounceState
	debounceMu    sync.Mutex
	debounceState map[int64]map[string]*DebounceEntry

	ctx    context.Context
	cancel context.CancelFunc
}

// DebounceEntry tracks occurrence counts within a 5-minute window.
type DebounceEntry struct {
	Count int
	Timer *time.Timer
}

// Global is the singleton alert manager for the platform.
var Global *AlertManager

// NewAlertManager creates a ready-to-run AlertManager.
// Call Start() to begin background processing.
func NewAlertManager(cli *client.Client) *AlertManager {
	ctx, cancel := context.WithCancel(context.Background())
	Global = &AlertManager{
		cli:           cli,
		rules:         make(map[int64]*AlertRule),
		lastTriggered: make(map[int64]time.Time),
		downState:     make(map[string]bool),
		activeTails:   make(map[string]context.CancelFunc),
		debounceState: make(map[int64]map[string]*DebounceEntry),
		ctx:           ctx,
		cancel:        cancel,
	}
	return Global
}

// Start loads rules from the database and spins up the two background loops.
// It is safe to call Start only once.
func (am *AlertManager) Start() {
	am.ReloadRules()
	db.OnAuditLogged = func(action, resource, status, details string) {
		am.TriggerSystemAlert("audit", fmt.Sprintf("Action: %s\nResource: %s\nStatus: %s\n%s", action, resource, status, details))
	}
	go am.listenToDockerEvents()
	go am.syncLogTailersLoop()
	go am.checkMetricsLoop()
}

// Stop shuts down all background goroutines cleanly. The root context is
// cancelled, which cascades to every log-tailer and the event listener.
func (am *AlertManager) Stop() {
	am.cancel()
	// Also explicitly cancel any tailers that may have been started after the
	// root context was created (belt-and-suspenders).
	am.tailsMu.Lock()
	for _, cancel := range am.activeTails {
		cancel()
	}
	am.tailsMu.Unlock()
}

// ReloadRules refreshes the in-memory rules cache from SQLite.
// It is safe to call concurrently and must be called after every CRUD
// operation on alert_rules so the engine stays in sync without a restart.
func (am *AlertManager) ReloadRules() {
	var dbRules []db.AlertRule
	if err := db.GormDB.Where("enabled = ?", true).Find(&dbRules).Error; err != nil {
		log.Printf("[Alerts] ReloadRules: query failed: %v", err)
		return
	}

	newRules := make(map[int64]*AlertRule)
	for _, dbR := range dbRules {
		r := &AlertRule{
			ID:               int64(dbR.ID),
			Name:             dbR.Name,
			ContainerPattern: dbR.ContainerPattern,
			EventTypes:       dbR.EventTypes,
			LogPattern:       dbR.LogPattern,
			Enabled:          dbR.Enabled,
			CooldownSeconds:  dbR.CooldownSeconds,
			EnableSlack:      dbR.EnableSlack,
			EnableMSTeams:    dbR.EnableMSTeams,
			EnableGChat:      dbR.EnableGChat,
			EnableGenericWebhook: dbR.EnableGenericWebhook,
			EnableEmail:      dbR.EnableEmail,
			EmailAddress:     dbR.EmailAddress,
			MetricCPUThreshold: dbR.MetricCpuThreshold,
			MetricMemThreshold: dbR.MetricMemThreshold,
		}
		newRules[r.ID] = r
	}

	am.rulesMu.Lock()
	am.rules = newRules
	am.rulesMu.Unlock()

	log.Printf("[Alerts] Rules reloaded — %d active rule(s)", len(newRules))

	// Adjust log tailers to reflect the new rule set immediately.
	go am.syncLogTailers()
}

// ─── Docker Event Listener ────────────────────────────────────────────────────

// listenToDockerEvents connects to the Docker daemon event stream and processes
// container lifecycle events (die, oom, health_status) against the rule cache.
// If the stream drops it automatically reconnects after a short backoff.
func (am *AlertManager) listenToDockerEvents() {
	for {
		select {
		case <-am.ctx.Done():
			return
		default:
		}

		am.runEventLoop()

		// Back off before reconnecting, unless we are shutting down.
		select {
		case <-am.ctx.Done():
			return
		case <-time.After(5 * time.Second):
			log.Printf("[Alerts] Reconnecting to Docker event stream…")
		}
	}
}

// runEventLoop is the inner event-processing loop. It returns when the stream
// closes or when the manager's root context is cancelled.
func (am *AlertManager) runEventLoop() {
	evCtx, evCancel := context.WithCancel(am.ctx)
	defer evCancel()

	eventRes := am.cli.Events(evCtx, client.EventsListOptions{})
	messages, errs := eventRes.Messages, eventRes.Err

	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				return
			}
			// We only care about container-level events.
			if msg.Type != events.ContainerEventType {
				continue
			}
			am.processContainerEvent(msg)

		case err, ok := <-errs:
			if !ok {
				return
			}
			if err != nil && err != context.Canceled {
				log.Printf("[Alerts] Docker event stream error: %v", err)
			}
			return

		case <-am.ctx.Done():
			return
		}
	}
}

// TriggerSystemAlert manually triggers a system-level alert.
// It iterates through all active rules, and if the rule's EventTypes contains
// the specified eventType, it dispatches an alert with the provided details.
func (am *AlertManager) TriggerSystemAlert(eventType string, details string) {
	if am == nil {
		return
	}
	am.rulesMu.RLock()
	defer am.rulesMu.RUnlock()

	for _, rule := range am.rules {
		if rule.EventTypes == "" {
			continue
		}
		// A system alert is not tied to a specific container, so we bypass ContainerPattern
		// and just check if the rule is listening for this event type.
		for _, ev := range splitTrim(rule.EventTypes, ",") {
			if ev == eventType {
				am.triggerAlert(rule, "System", "system", details)
				break
			}
		}
	}
}

// TriggerContainerEvent manually triggers an event tied to a specific container.
// This is used by external subsystems (e.g. vulnerability scans) to emit alerts
// that correctly evaluate against the rule's ContainerPattern.
func (am *AlertManager) TriggerContainerEvent(eventType string, containerName string, details string) {
	if am == nil {
		return
	}
	am.rulesMu.RLock()
	defer am.rulesMu.RUnlock()

	for _, rule := range am.rules {
		if rule.EventTypes == "" {
			continue
		}

		matched, err := regexp.MatchString(rule.ContainerPattern, containerName)
		if err != nil || !matched {
			continue
		}

		for _, ev := range splitTrim(rule.EventTypes, ",") {
			if ev == eventType {
				am.triggerAlert(rule, containerName, "event", details)
				break
			}
		}
	}
}

// processContainerEvent evaluates a single Docker event against every enabled
// rule that (a) targets the container via ContainerPattern and (b) lists the
// event action in EventTypes.
func (am *AlertManager) processContainerEvent(msg events.Message) { //nolint:gocritic
	containerName := strings.TrimPrefix(msg.Actor.Attributes["name"], "/")
	action := string(msg.Action)

	// When a container starts, re-sync log tailers so we begin scanning the
	// new container's log stream immediately.
	if action == "start" {
		go am.syncLogTailers()
	}

	am.rulesMu.RLock()
	defer am.rulesMu.RUnlock()

	var recovered bool
	if action == "start" {
		am.downStateMu.Lock()
		recovered = am.downState[containerName]
		if recovered {
			delete(am.downState, containerName)
		}
		am.downStateMu.Unlock()
	}

	for _, rule := range am.rules {
		if rule.EventTypes == "" {
			continue
		}

		matched, err := regexp.MatchString(rule.ContainerPattern, containerName)
		if err != nil || !matched {
			continue
		}

		if action == "start" && recovered {
			am.triggerAlert(rule, containerName, "recovery", "Container started successfully after a crash/downtime.")
			continue
		}

		// Walk the comma-separated event list; support "health_status" prefix
		// matching (the Docker action is "health_status: healthy" etc.).
		for _, ev := range splitTrim(rule.EventTypes, ",") {
			switch {
			case ev == action || (ev == "health_status" && strings.HasPrefix(action, "health_status")):
				if action == "die" || action == "oom" || action == "kill" || action == "stop" || strings.Contains(action, "unhealthy") {
					am.downStateMu.Lock()
					am.downState[containerName] = true
					am.downStateMu.Unlock()
				}

				details := "Docker event: " + action
				if exitCode, ok := msg.Actor.Attributes["exitCode"]; ok && exitCode != "" {
					details += " (exit code " + exitCode + ")"
				}
				am.triggerAlert(rule, containerName, "event", details)
			}
		}
	}
}

// ─── Lazy Log Tailer ─────────────────────────────────────────────────────────

// syncLogTailersLoop periodically calls syncLogTailers to handle containers
// that appeared after the initial startup and containers whose rules changed.
func (am *AlertManager) syncLogTailersLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-am.ctx.Done():
			return
		case <-ticker.C:
			am.syncLogTailers()
		}
	}
}

// syncLogTailers reconciles the set of active log-tailer goroutines with the
// current rule cache and the list of running containers.
//
// For each running container that matches at least one log-pattern rule, exactly
// one tailer goroutine is maintained. Tailers for containers that are no longer
// running or no longer matched by any rule are cancelled immediately.
func (am *AlertManager) syncLogTailers() {
	// Snapshot the rules so we don't hold rulesMu across slow Docker API calls.
	am.rulesMu.RLock()
	logRules := make([]*AlertRule, 0, len(am.rules))
	for _, r := range am.rules {
		if r.LogPattern != "" {
			logRules = append(logRules, r)
		}
	}
	am.rulesMu.RUnlock()

	if am.cli == nil {
		return
	}

	// Fetch running containers.
	listResult, err := am.cli.ContainerList(am.ctx, client.ContainerListOptions{})
	if err != nil {
		log.Printf("[Alerts] syncLogTailers: ContainerList error: %v", err)
		return
	}

	// Build a map of containers that need a tailer:
	// containerID → containerName.
	needTailer := make(map[string]string)
	for _, ctr := range listResult.Items {
		name := ""
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}
		for _, rule := range logRules {
			matched, err := regexp.MatchString(rule.ContainerPattern, name)
			if err == nil && matched {
				needTailer[ctr.ID] = name
				break
			}
		}
	}

	am.tailsMu.Lock()
	defer am.tailsMu.Unlock()

	// Cancel tailers for containers that are no longer needed.
	for id, cancel := range am.activeTails {
		if _, needed := needTailer[id]; !needed {
			log.Printf("[Alerts] Stopping log tailer for container %s", id[:12])
			cancel()
			delete(am.activeTails, id)
		}
	}

	// Start tailers for containers that don't have one yet.
	for id, name := range needTailer {
		if _, active := am.activeTails[id]; !active {
			tailCtx, tailCancel := context.WithCancel(am.ctx)
			am.activeTails[id] = tailCancel
			log.Printf("[Alerts] Starting log tailer for container %s (%s)", id[:12], name)
			go am.tailContainerLogs(tailCtx, id, name)
		}
	}
}

// tailContainerLogs reads the live log stream for a single container and
// matches every line against the log-pattern rules that target it.
//
// The goroutine exits cleanly when:
//   - tailCtx is cancelled (Stop(), rule deletion, container stop detected)
//   - the Docker log stream returns io.EOF (container exited)
//   - any unrecoverable read error occurs
func (am *AlertManager) tailContainerLogs(tailCtx context.Context, containerID, containerName string) {
	// Remove ourselves from the active-tailers map when we exit so that
	// syncLogTailers can spawn a replacement if the container restarts.
	defer func() {
		am.tailsMu.Lock()
		delete(am.activeTails, containerID)
		am.tailsMu.Unlock()
	}()

	logResult, err := am.cli.ContainerLogs(tailCtx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		// Tail "0" means "start from now" — we never alert on historical lines.
		Tail: "0",
	})
	if err != nil {
		// Context cancelled = clean shutdown; anything else is worth logging.
		if tailCtx.Err() == nil {
			log.Printf("[Alerts] tailContainerLogs(%s): ContainerLogs error: %v", containerName, err)
		}
		return
	}
	defer logResult.Close()

	// Docker multiplexes stdout and stderr into a single stream with an 8-byte
	// frame header: [stream_type(1)] [padding(3)] [size(4 big-endian)].
	// This is the same demux logic used by the main WebSocket log handler.
	header := make([]byte, 8)

	for {
		// Check context first so a cancelled tailer exits without blocking on Read.
		select {
		case <-tailCtx.Done():
			return
		default:
		}

		_, err := io.ReadFull(logResult, header)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF && tailCtx.Err() == nil {
				log.Printf("[Alerts] tailContainerLogs(%s): header read error: %v", containerName, err)
			}
			return
		}

		frameSize := uint32(header[4])<<24 | uint32(header[5])<<16 |
			uint32(header[6])<<8 | uint32(header[7])

		if frameSize == 0 {
			continue
		}

		payload := make([]byte, frameSize)
		_, err = io.ReadFull(logResult, payload)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF && tailCtx.Err() == nil {
				log.Printf("[Alerts] tailContainerLogs(%s): payload read error: %v", containerName, err)
			}
			return
		}

		line := strings.TrimRight(string(payload), "\r\n")
		if line == "" {
			continue
		}

		am.evaluateLogLine(containerName, line)
	}
}

// evaluateLogLine checks a single log line against every log-pattern rule that
// targets the given container.
func (am *AlertManager) evaluateLogLine(containerName, line string) {
	am.rulesMu.RLock()
	defer am.rulesMu.RUnlock()

	for _, rule := range am.rules {
		if rule.LogPattern == "" {
			continue
		}
		matched, err := regexp.MatchString(rule.ContainerPattern, containerName)
		if err != nil || !matched {
			continue
		}
		logMatched, err := regexp.MatchString(rule.LogPattern, line)
		if err != nil || !logMatched {
			continue
		}

		// Truncate very long lines so the notification stays readable.
		detail := line
		if len(detail) > 500 {
			detail = detail[:500] + "…"
		}
		am.triggerAlert(rule, containerName, "log", detail)
	}
}

// ─── Trigger & Cooldown ───────────────────────────────────────────────────────

func (am *AlertManager) triggerAlert(rule *AlertRule, containerName, alertType, details string) {
	am.debounceMu.Lock()
	defer am.debounceMu.Unlock()

	if am.debounceState[rule.ID] == nil {
		am.debounceState[rule.ID] = make(map[string]*DebounceEntry)
	}

	entry, ok := am.debounceState[rule.ID][containerName]
	if !ok {
		// Respect original cooldown for creating the first entry of the window.
		// We do it inside the mutex to avoid race conditions.
		am.ltMu.Lock()
		last, seen := am.lastTriggered[rule.ID]
		canTrigger := !seen || time.Since(last) >= time.Duration(rule.CooldownSeconds)*time.Second
		if canTrigger {
			am.lastTriggered[rule.ID] = time.Now()
		}
		am.ltMu.Unlock()

		if !canTrigger {
			return
		}

		entry = &DebounceEntry{Count: 1}
		am.debounceState[rule.ID][containerName] = entry

		entry.Timer = time.AfterFunc(5*time.Minute, func() {
			am.debounceMu.Lock()
			count := entry.Count
			delete(am.debounceState[rule.ID], containerName)
			am.debounceMu.Unlock()

			enrichedDetails := details
			if count > 1 {
				enrichedDetails = fmt.Sprintf("%s (x%d occurrences in last 5m)", details, count)
			}
			am.deliverAlert(rule, containerName, alertType, enrichedDetails)
		})
	} else {
		entry.Count++
	}
}

// deliverAlert persists the alert to history and dispatches notifications.
func (am *AlertManager) deliverAlert(rule *AlertRule, containerName, alertType, details string) {
	history := db.AlertHistory{
		RuleID:        func(i uint) *uint { return &i }(uint(rule.ID)),
		RuleName:      rule.Name,
		ContainerName: containerName,
		AlertType:     alertType,
		Details:       details,
	}
	if err := db.GormDB.Create(&history).Error; err != nil {
		log.Printf("[Alerts] Failed to record alert_history for rule %d: %v", rule.ID, err)
	}

	var setting db.Setting
	err := db.GormDB.First(&setting, 1).Error
	if err != nil {
		log.Printf("[Alerts] Failed to fetch setting: %v", err)
	}

	var teams []db.Team
	db.GormDB.Find(&teams)

	slackURLs := make(map[string]bool)
	msteamsURLs := make(map[string]bool)
	gchatURLs := make(map[string]bool)
	genericURLs := make(map[string]bool)
	emails := make(map[string]bool)

	if setting.SlackWebhookUrl != "" { slackURLs[setting.SlackWebhookUrl] = true }
	if setting.MSTeamsWebhookUrl != "" { msteamsURLs[setting.MSTeamsWebhookUrl] = true }
	if setting.GChatWebhookUrl != "" { gchatURLs[setting.GChatWebhookUrl] = true }
	if setting.GenericWebhookUrl != "" { genericURLs[setting.GenericWebhookUrl] = true }
	if setting.AlertsEmailAddress != "" { emails[setting.AlertsEmailAddress] = true }

	if setting.AlertsEmailAddress == "" && rule.EmailAddress != "" {
		emails[rule.EmailAddress] = true
	}

	for _, team := range teams {
		if team.AllowedContainers != "" {
			matched, err := regexp.MatchString(team.AllowedContainers, containerName)
			if err == nil && matched {
				if team.SlackWebhookUrl != "" { slackURLs[team.SlackWebhookUrl] = true }
				if team.MSTeamsWebhookUrl != "" { msteamsURLs[team.MSTeamsWebhookUrl] = true }
				if team.GChatWebhookUrl != "" { gchatURLs[team.GChatWebhookUrl] = true }
				if team.GenericWebhookUrl != "" { genericURLs[team.GenericWebhookUrl] = true }
				if team.AlertsEmailAddress != "" { emails[team.AlertsEmailAddress] = true }
			}
		}
	}

	payload := NotificationPayload{
		RuleName:      rule.Name,
		ContainerName: containerName,
		Type:          alertType,
		Details:       details,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}

	go func() {
		var statusMsgs []string
		var channels []string
		var channelsMu sync.Mutex

		deliverCh := func(channelType, url string, enabled bool) {
			if !enabled {
				return
			}
			channelsMu.Lock()
			channels = append(channels, channelType)
			channelsMu.Unlock()

			cfgMap := map[string]string{"url": url}
			cfgBytes, _ := json.Marshal(cfgMap)
			if err := DeliverNotification(channelType, string(cfgBytes), payload); err != nil {
				log.Printf("[Alerts] Delivery failed for rule %q (%s): %v", payload.RuleName, channelType, err)
				channelsMu.Lock()
				statusMsgs = append(statusMsgs, fmt.Sprintf("%s Failed: %v", channelType, err))
				channelsMu.Unlock()
			} else {
				channelsMu.Lock()
				statusMsgs = append(statusMsgs, fmt.Sprintf("%s Success", channelType))
				channelsMu.Unlock()
			}
		}

		var wg sync.WaitGroup
		for url := range slackURLs {
			wg.Add(1)
			go func(u string) { defer wg.Done(); deliverCh("slack", u, rule.EnableSlack) }(url)
		}
		for url := range msteamsURLs {
			wg.Add(1)
			go func(u string) { defer wg.Done(); deliverCh("msteams", u, rule.EnableMSTeams) }(url)
		}
		for url := range gchatURLs {
			wg.Add(1)
			go func(u string) { defer wg.Done(); deliverCh("gchat", u, rule.EnableGChat) }(url)
		}
		for url := range genericURLs {
			wg.Add(1)
			go func(u string) { defer wg.Done(); deliverCh("generic_webhook", u, rule.EnableGenericWebhook) }(url)
		}

		if rule.EnableEmail && len(emails) > 0 {
			if setting.SmtpHost == "" {
				log.Printf("[Alerts] Cannot send email: SMTP Host not configured")
				channelsMu.Lock()
				channels = append(channels, "email")
				statusMsgs = append(statusMsgs, "Email Failed: SMTP Host not configured")
				channelsMu.Unlock()
			} else {
				var toAddr string
				var ccAddrs []string

				if setting.AlertsEmailAddress != "" {
					toAddr = setting.AlertsEmailAddress
				}

				for e := range emails {
					if toAddr == "" {
						toAddr = e
					} else if e != toAddr {
						ccAddrs = append(ccAddrs, e)
					}
				}

				wg.Add(1)
				go func() {
					defer wg.Done()
					channelsMu.Lock()
					channels = append(channels, "email")
					channelsMu.Unlock()

					log.Printf("[Alerts] Sending email to %s (CC: %v) for rule %q", toAddr, ccAddrs, payload.RuleName)
					err := DeliverEmail(setting.SmtpHost, setting.SmtpPort, setting.SmtpUser, setting.SmtpPass, toAddr, ccAddrs, payload)
					
					channelsMu.Lock()
					if err != nil {
						log.Printf("[Alerts] Email delivery failed for rule %q: %v", payload.RuleName, err)
						statusMsgs = append(statusMsgs, fmt.Sprintf("Email Failed: %v", err))
					} else {
						statusMsgs = append(statusMsgs, "Email Success")
					}
					channelsMu.Unlock()
				}()
			}
		}

		wg.Wait()

		if len(statusMsgs) > 0 {
			db.GormDB.Model(&history).Updates(map[string]interface{}{
				"delivery_status":  strings.Join(statusMsgs, " | "),
				"delivery_channel": strings.Join(channels, " | "),
			})
		} else {
			db.GormDB.Model(&history).Updates(map[string]interface{}{
				"delivery_status":  "Not Delivered (No Channels Enabled)",
				"delivery_channel": "None",
			})
		}
	}()
}

// checkCooldown returns true if the rule may fire right now (i.e., its cooldown
// window has elapsed) and updates the timestamp atomically.
// Returns false if the rule is still on cooldown.
func (am *AlertManager) checkCooldown(ruleID int64, cooldownSeconds int) bool {
	am.ltMu.Lock()
	defer am.ltMu.Unlock()

	last, seen := am.lastTriggered[ruleID]
	if seen && time.Since(last) < time.Duration(cooldownSeconds)*time.Second {
		return false
	}
	am.lastTriggered[ruleID] = time.Now()
	return true
}

// ─── Metric Deviation Tracking ────────────────────────────────────────────────

// checkMetricsLoop periodically queries the stats table for baseline deviations
// according to user thresholds (e.g. MetricCPUThreshold, MetricMemThreshold).
func (am *AlertManager) checkMetricsLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-am.ctx.Done():
			return
		case <-ticker.C:
			am.evaluateMetrics()
		}
	}
}

func (am *AlertManager) evaluateMetrics() {
	am.rulesMu.RLock()
	activeRules := make([]*AlertRule, 0, len(am.rules))
	for _, r := range am.rules {
		if r.Enabled && (r.MetricCPUThreshold > 0 || r.MetricMemThreshold > 0) {
			activeRules = append(activeRules, r)
		}
	}
	am.rulesMu.RUnlock()

	if len(activeRules) == 0 {
		return
	}

	// For baseline tracking, we need the 1-hour average and current reading.
	// Query the most recent stats per container.
	var stats []db.Stat
	err := db.GormDB.Where("timestamp >= ?", time.Now().Add(-2*time.Minute)).
		Order("timestamp DESC").Find(&stats).Error
	if err != nil {
		log.Printf("[Alerts] evaluateMetrics: failed to query recent stats: %v", err)
		return
	}


	recentStats := make(map[string]db.Stat)
	seen := make(map[string]bool)
	for _, stat := range stats {
		if !seen[stat.ContainerID] {
			recentStats[stat.ContainerID] = stat
			seen[stat.ContainerID] = true
		}
	}

	// Get 1-hour baseline average
	var baselineStats []db.Stat
	err = db.GormDB.Where("timestamp >= ?", time.Now().Add(-65*time.Minute)).
		Where("timestamp <= ?", time.Now().Add(-55*time.Minute)).
		Order("timestamp DESC").Find(&baselineStats).Error
	if err != nil {
		log.Printf("[Alerts] evaluateMetrics: failed to query baseline stats: %v", err)
		return
	}

	baseline := make(map[string]db.Stat)
	bSeen := make(map[string]bool)
	for _, stat := range baselineStats {
		if !bSeen[stat.ContainerID] {
			baseline[stat.ContainerID] = stat
			bSeen[stat.ContainerID] = true
		}
	}

	// For each container in recentStats, check against applicable rules
	// For resolving container_id to name: we can get names from am.cli if needed,
	// but container_id in DB is usually the ID.
	// To map container_id to name, let's fetch containers.
	listResult, err := am.cli.ContainerList(am.ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return
	}

	idToName := make(map[string]string)
	for _, c := range listResult.Items {
		name := strings.TrimPrefix(c.Names[0], "/")
		idToName[c.ID[:12]] = name // the stats usually store short ID or full ID
		idToName[c.ID] = name
	}

	for cid, current := range recentStats {
		cName, ok := idToName[cid]
		if !ok {
			cName = cid
		}

		baselineStat, hasBaseline := baseline[cid]
		if !hasBaseline {
			continue // Need historical data to establish baseline
		}

		for _, rule := range activeRules {
			matched, err := regexp.MatchString(rule.ContainerPattern, cName)
			if err != nil || !matched {
				continue
			}

			// CPU Check: Current > user threshold AND Current > 1.5x Baseline
			if rule.MetricCPUThreshold > 0 && current.CPU > rule.MetricCPUThreshold {
				if current.CPU > (baselineStat.CPU * 1.5) {
					details := fmt.Sprintf("High CPU Detected: %.2f%% (Threshold: %.2f%%, 1h Baseline: %.2f%%)", current.CPU, rule.MetricCPUThreshold, baselineStat.CPU)
					am.triggerAlert(rule, cName, "metric_cpu", details)
				}
			}

			// Mem Check: Current > user threshold (MB) AND Current > 1.5x Baseline
			// Mem in DB is usually bytes or MB depending on how collectStats saves it.
			// Let's assume stats in DB is bytes, rule is MB.
			currentMemMB := float64(current.Memory) / 1024 / 1024
			baselineMemMB := float64(baselineStat.Memory) / 1024 / 1024
			ruleMemMB := float64(rule.MetricMemThreshold)

			if rule.MetricMemThreshold > 0 && currentMemMB > ruleMemMB {
				if currentMemMB > (baselineMemMB * 1.5) {
					details := fmt.Sprintf("High Memory Detected: %.2f MB (Threshold: %.2f MB, 1h Baseline: %.2f MB)", currentMemMB, ruleMemMB, baselineMemMB)
					am.triggerAlert(rule, cName, "metric_mem", details)
				}
			}
		}
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// splitTrim splits s by sep and trims whitespace from each element.
func splitTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
