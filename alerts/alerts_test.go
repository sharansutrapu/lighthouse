package alerts

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"lighthouse/db"
	"lighthouse/scanner"

	"context"

	"github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"gorm.io/gorm"
)

type mockTransport struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func setupTestManager(t *testing.T) *AlertManager {
	// Initialize a fresh in-memory SQLite for DB dependencies
	var err error
	db.GormDB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	db.GormDB.AutoMigrate(&db.AlertRule{}, &db.AlertHistory{}, &db.Setting{}, &db.ImageScanResult{})
	db.GormDB.Create(&db.Setting{ID: 1}) // Needed for deliverAlert

	am := NewAlertManager(nil)
	return am
}

func TestReloadRules(t *testing.T) {
	am := setupTestManager(t)
	defer am.Stop()

	// Initially no rules
	am.rulesMu.RLock()
	count := len(am.rules)
	am.rulesMu.RUnlock()
	if count != 0 {
		t.Fatalf("Expected 0 rules, got %d", count)
	}

	// Add rule to DB
	db.GormDB.Create(&db.AlertRule{
		Name:             "Test Rule",
		ContainerPattern: ".*",
		Enabled:          true,
	})

	am.ReloadRules()

	am.rulesMu.RLock()
	count = len(am.rules)
	am.rulesMu.RUnlock()
	if count != 1 {
		t.Fatalf("Expected 1 rule after reload, got %d", count)
	}
}

func TestCooldownLogic(t *testing.T) {
	am := setupTestManager(t)
	defer am.Stop()

	ruleID := int64(999)
	cooldown := 2 // 2 seconds
	containerA := "container-a"

	// First trigger should pass
	if !am.checkCooldown(ruleID, containerA, cooldown) {
		t.Fatal("Expected checkCooldown to be true on first call")
	}

	// Immediate second trigger on same container should fail
	if am.checkCooldown(ruleID, containerA, cooldown) {
		t.Fatal("Expected checkCooldown to be false immediately after first call")
	}

	// Wait for cooldown
	time.Sleep(3 * time.Second)

	// Third trigger should pass after cooldown expires
	if !am.checkCooldown(ruleID, containerA, cooldown) {
		t.Fatal("Expected checkCooldown to be true after cooldown expires")
	}
}

func TestCooldownPerContainerIsolation(t *testing.T) {
	am := setupTestManager(t)
	defer am.Stop()

	ruleID := int64(888)
	cooldown := 60 // 60 seconds
	containerA := "container-a"
	containerB := "container-b"

	// Trigger for container A — should pass
	if !am.checkCooldown(ruleID, containerA, cooldown) {
		t.Fatal("Expected containerA first call to pass")
	}

	// Container A is in cooldown but Container B should NOT be affected
	if !am.checkCooldown(ruleID, containerB, cooldown) {
		t.Fatal("Expected containerB to pass even while containerA is in cooldown — cooldowns must be per-container")
	}

	// Container A should still be in cooldown
	if am.checkCooldown(ruleID, containerA, cooldown) {
		t.Fatal("Expected containerA to still be in cooldown")
	}
}

func TestWebhookDelivery(t *testing.T) {
	received := false
	origTransport := httpClient.Transport
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			received = true
			body, _ := io.ReadAll(req.Body)
			var payload NotificationPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("Failed to parse payload: %v", err)
			}
			if payload.RuleName != "Test Webhook Rule" {
				t.Errorf("Expected rule name 'Test Webhook Rule', got %s", payload.RuleName)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(nil)),
				Header:     make(http.Header),
			}, nil
		},
	}
	defer func() { httpClient.Transport = origTransport }()

	payload := NotificationPayload{
		RuleName:      "Test Webhook Rule",
		ContainerName: "test-container",
		Type:          "test",
		Details:       "This is a test alert",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	configJSON := `{"url":"http://dummy"}`
	err := DeliverNotification("generic_webhook", configJSON, payload)
	if err != nil {
		t.Fatalf("DeliverNotification failed: %v", err)
	}

	if !received {
		t.Fatal("Test server did not receive webhook")
	}
}

func TestEvaluateLogLine(t *testing.T) {
	am := setupTestManager(t)
	defer am.Stop()

	// Mock DB rule
	rule := &db.AlertRule{
		Name:             "Error Log",
		ContainerPattern: "^test-.*",
		LogPattern:       "ERROR",
		Enabled:          true,
	}
	db.GormDB.Create(rule)
	am.ReloadRules()

	// Trigger evaluate - this invokes debounce and triggerAlert
	am.evaluateLogLine("test-app", "This is an ERROR line")

	// Wait a tiny bit for debounce setup
	time.Sleep(100 * time.Millisecond)

	am.debounceMu.Lock()
	group, ok := am.groupedDebounce["test-app"]
	var entry *TriggeredRule
	if ok && group != nil {
		entry = group.Triggers[int64(rule.ID)]
	}
	am.debounceMu.Unlock()

	if entry == nil {
		t.Fatal("Expected log line to trigger debounce entry")
	}

	if entry.Count != 1 {
		t.Errorf("Expected debounce count 1, got %d", entry.Count)
	}

	// Should not match container pattern
	am.evaluateLogLine("other-app", "This is an ERROR line")

	// Should not match log pattern
	am.evaluateLogLine("test-app", "This is an INFO line")

	am.debounceMu.Lock()
	groupOther, okOther := am.groupedDebounce["other-app"]
	am.debounceMu.Unlock()

	if okOther && groupOther != nil {
		t.Fatal("Expected other app to NOT trigger debounce entry")
	}
}
func TestScanThrottle(t *testing.T) {
	am := setupTestManager(t)
	defer am.Stop()

	image := "myapp:latest"

	// First entry should not exist → should be allowed to scan
	am.scanThrottleMu.Lock()
	_, scanned := am.activeScans[image]
	am.scanThrottleMu.Unlock()
	if scanned {
		t.Fatal("Expected no scan entry initially")
	}

	// Simulate recording a scan
	am.scanThrottleMu.Lock()
	am.activeScans[image] = time.Now()
	am.scanThrottleMu.Unlock()

	// Now a second scan within 30 minutes should be throttled
	am.scanThrottleMu.Lock()
	lastScan, scanned := am.activeScans[image]
	shouldSkip := scanned && time.Since(lastScan) < 30*time.Minute
	am.scanThrottleMu.Unlock()

	if !shouldSkip {
		t.Fatal("Expected scan to be throttled within 30 minutes")
	}
}

func TestStartedAtGracePeriod(t *testing.T) {
	am := setupTestManager(t)
	defer am.Stop()

	container := "freshly-started"

	// Record start time as now
	am.startedAtMu.Lock()
	am.startedAt[container] = time.Now()
	am.startedAtMu.Unlock()

	// Immediately check — should be in grace period
	am.startedAtMu.Lock()
	startT, hasStart := am.startedAt[container]
	inGrace := hasStart && time.Since(startT) < 2*time.Minute
	am.startedAtMu.Unlock()

	if !inGrace {
		t.Fatal("Expected container to be in grace period immediately after start")
	}

	// Simulate an old start (3 minutes ago)
	am.startedAtMu.Lock()
	am.startedAt[container] = time.Now().Add(-3 * time.Minute)
	am.startedAtMu.Unlock()

	am.startedAtMu.Lock()
	startT, hasStart = am.startedAt[container]
	inGrace = hasStart && time.Since(startT) < 2*time.Minute
	am.startedAtMu.Unlock()

	if inGrace {
		t.Fatal("Expected container to NOT be in grace period 3 minutes after start")
	}
}

func TestDebounceCountIncrement(t *testing.T) {
	am := setupTestManager(t)
	defer am.Stop()

	rule := &db.AlertRule{
		Name:             "Debounce Count Test",
		ContainerPattern: "^myapp$",
		LogPattern:       "FAIL",
		Enabled:          true,
	}
	db.GormDB.Create(rule)
	am.ReloadRules()

	// Trigger same log match 3 times
	am.evaluateLogLine("myapp", "FAIL: something bad")
	am.evaluateLogLine("myapp", "FAIL: something bad again")
	am.evaluateLogLine("myapp", "FAIL: third time")

	time.Sleep(50 * time.Millisecond)

	am.debounceMu.Lock()
	group, ok := am.groupedDebounce["myapp"]
	var count int
	if ok && group != nil {
		if tr, exists := group.Triggers[int64(rule.ID)]; exists {
			count = tr.Count
		}
	}
	am.debounceMu.Unlock()

	if count != 3 {
		t.Errorf("Expected debounce count 3 for repeated log matches, got %d", count)
	}
}

func TestAutoScanOnContainerStart(t *testing.T) {
	am := setupTestManager(t)

	// Enable AutoScan in DB
	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("auto_scan_enabled", true)

	// Mock scanner
	originalScanImageFunc := scanner.ScanImageFunc
	defer func() { scanner.ScanImageFunc = originalScanImageFunc }()

	scanExecuted := false
	scanner.ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
		scanExecuted = true
		return map[string]interface{}{}, nil
	}

	// Trigger container start event
	event := events.Message{
		Action: "start",
		Actor: events.Actor{
			Attributes: map[string]string{
				"name":  "test-container",
				"image": "test-image:latest",
			},
		},
	}
	am.processContainerEvent(event)

	time.Sleep(100 * time.Millisecond)

	if !scanExecuted {
		t.Errorf("Expected auto-scan to execute for started container")
	}

	// Test Throttle (30 minutes)
	scanExecuted = false
	am.processContainerEvent(event)
	time.Sleep(100 * time.Millisecond)
	if scanExecuted {
		t.Errorf("Expected auto-scan to be throttled for the same image")
	}
}

// --- Merged from coverage_extra4_test.go ---

func TestAuditLogCallback(t *testing.T) {
	setupTestDBExtra(t)
	am := NewAlertManager(nil)
	am.Start()
	db.OnAuditLogged("action", "resource", "status", "details")
	am.Stop()
}

func TestReloadRulesRegexErrors(t *testing.T) {
	setupTestDBExtra(t)
	db.GormDB.Create(&db.AlertRule{
		Name:             "Bad Container",
		ContainerPattern: "[invalid",
		Enabled:          true,
	})
	db.GormDB.Create(&db.AlertRule{
		Name:             "Bad Log",
		ContainerPattern: ".*",
		LogPattern:       "[invalid",
		Enabled:          true,
	})

	am := NewAlertManager(nil)
	am.ReloadRules()
}

func TestProcessContainerEventFull(t *testing.T) {
	am := NewAlertManager(nil)
	am.rules = map[int64]*AlertRule{
		1: {
			EventTypes:       "start,die,health_status",
			ContainerPattern: ".*",
		},
	}

	// Test without name but with ID
	am.processContainerEvent(events.Message{
		Action: "die",
		Actor: events.Actor{
			Attributes: map[string]string{},
			ID:         "1234567890123456",
		},
	})

	// Test down state and recovery
	am.processContainerEvent(events.Message{
		Action: "die",
		Actor: events.Actor{
			Attributes: map[string]string{"name": "test"},
		},
	})
	am.processContainerEvent(events.Message{
		Action: "start",
		Actor: events.Actor{
			Attributes: map[string]string{"name": "test"},
		},
	})
}

func TestModelsNilRegex(t *testing.T) {
	rule := &AlertRule{}
	rule.matchesContainer("test")
	rule.matchesLog("test")
}

// --- Merged from coverage_extra_test.go ---

func TestEvaluateStorageMetricsFull(t *testing.T) {
	setupTestDBExtra(t)
	am := NewAlertManager(nil)
	am.rules = map[int64]*AlertRule{
		1: {
			ID:                     1,
			Name:                   "System Storage",
			ContainerPattern:       "system",
			MetricStorageThreshold: 1, // Will definitely trigger
			MetricMemThreshold:     1, // Will definitely trigger
			Enabled:                true,
		},
	}

	activeRules := []*AlertRule{am.rules[1]}
	am.evaluateStorageMetrics(activeRules)
	// We just ensure it doesn't panic. We can't mock host stats easily here without an interface, but we can call it.
}

func TestSyncLogTailersFull(t *testing.T) {
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/containers/json") {
					containers := []map[string]interface{}{
						{
							"Id":    "111111111111",
							"Names": []string{"/test-container-1"},
						},
						{
							"Id":    "222222222222",
							"Names": []string{"/test-container-2"},
						},
					}
					b, _ := json.Marshal(containers)
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewReader(b)),
					}, nil
				}
				if strings.Contains(req.URL.Path, "/logs") {
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewBuffer(nil)),
					}, nil
				}
				return &http.Response{
					StatusCode: 500,
					Body:       io.NopCloser(bytes.NewBuffer(nil)),
				}, nil
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))

	am := &AlertManager{
		cli: cli,
		ctx: context.Background(),
		rules: map[int64]*AlertRule{
			1: {
				ID:               1,
				ContainerPattern: ".*",
				LogPattern:       "error",
			},
		},
		activeTails: make(map[string]context.CancelFunc),
	}

	// Pre-populate one that should be cancelled
	am.activeTails["333333333333"] = func() {}

	am.syncLogTailers()
}

func TestTailContainerLogsEOF(t *testing.T) {
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))
	am := &AlertManager{
		cli:         cli,
		tailsMu:     sync.Mutex{},
		activeTails: make(map[string]context.CancelFunc),
	}

	ctx, cancel := context.WithCancel(context.Background())
	am.activeTails["test"] = cancel

	am.tailContainerLogs(ctx, "test", "test")
}

func TestDeliverGroupFull(t *testing.T) {
	setupTestDBExtra(t)
	db.GormDB.Create(&db.Setting{
		ID:                 1,
		SmtpHost:           "dummy",
		AlertsEmailAddress: "test@example.com",
		SlackWebhookUrl:    "http://dummy",
		MSTeamsWebhookUrl:  "http://dummy",
		GChatWebhookUrl:    "http://dummy",
		GenericWebhookUrl:  "http://dummy",
	})

	db.GormDB.Create(&db.Team{
		Name:               "Test Team",
		AllowedContainers:  "test-.*",
		SlackWebhookUrl:    "http://team-slack",
		MSTeamsWebhookUrl:  "http://team-msteams",
		GChatWebhookUrl:    "http://team-gchat",
		GenericWebhookUrl:  "http://team-generic",
		AlertsEmailAddress: "team@example.com",
	})

	am := &AlertManager{
		rules: map[int64]*AlertRule{},
	}

	rule := &AlertRule{
		ID:                   1,
		Name:                 "Test Rule",
		EnableSlack:          true,
		EnableMSTeams:        true,
		EnableGChat:          true,
		EnableGenericWebhook: true,
		EnableEmail:          true,
		EmailAddress:         "rule@example.com",
	}

	triggers := []TriggeredRule{
		{
			Rule:      rule,
			AlertType: "event",
			Details:   "Details here",
			Count:     2,
		},
	}

	am.deliverGroup("test-container", triggers)
	time.Sleep(200 * time.Millisecond) // Wait for go routines to fire
}

func TestDeliverEmailFull(t *testing.T) {
	payload := NotificationPayload{
		Type:          "test",
		ContainerName: "test",
		RuleName:      "test",
		Details:       "test",
	}
	// Missing recipients
	err := DeliverEmail("127.0.0.1", 587, "user", "pass", "", []string{}, payload)
	if err == nil {
		t.Fatal("Expected error for no recipients")
	}

	// Invalid TLS (port 465) connects to nothing
	err = DeliverEmail("127.0.0.1", 465, "user", "pass", "to@example.com", []string{"cc@example.com"}, payload)
	if err == nil {
		t.Fatal("Expected error connecting to 465")
	}
}

func TestIsAllowedWebhookURLCoverage(t *testing.T) {
	if isAllowedWebhookURL("not-a-url") {
		t.Fatal("Expected not-a-url to be blocked")
	}
	if isAllowedWebhookURL("ftp://1.1.1.1") {
		t.Fatal("Expected ftp to be blocked")
	}
	if isAllowedWebhookURL("http://169.254.169.254") {
		t.Fatal("Expected AWS metadata to be blocked")
	}
	if isAllowedWebhookURL("http://metadata.google.internal") {
		t.Fatal("Expected GCP metadata to be blocked")
	}
	if isAllowedWebhookURL("http://127.0.0.1") {
		t.Fatal("Expected localhost to be blocked")
	}
	if isAllowedWebhookURL("http://10.0.0.1") {
		t.Fatal("Expected private ip to be blocked")
	}
}

func TestRunEventLoopErrors(t *testing.T) {
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	am := &AlertManager{
		cli: cli,
		ctx: ctx,
	}
	// Should return immediately on error
	am.runEventLoop()
}

func TestTriggerAlertWithRule(t *testing.T) {
	am := NewAlertManager(nil)
	rule := &AlertRule{
		ID:              1,
		CooldownSeconds: 60,
	}

	am.triggerAlert(rule, "container1", "log", "test")
	am.triggerAlert(rule, "container1", "log", "test 2")

	time.Sleep(50 * time.Millisecond) // Let it queue
}

// --- Merged from coverage_extra5_test.go ---

func TestDeliveryJSONErrors(t *testing.T) {
	oldJSON := jsonMarshal
	defer func() { jsonMarshal = oldJSON }()
	jsonMarshal = func(v interface{}) ([]byte, error) {
		return nil, errors.New("mock json error")
	}

	_ = sendSlackWebhook("http://dummy", NotificationPayload{})
	_ = sendMSTeamsWebhook("http://dummy", NotificationPayload{})
	_ = sendGChatWebhook("http://dummy", NotificationPayload{})
	_ = sendGenericWebhook("http://dummy", NotificationPayload{})
}

func TestDeliveryLookupError(t *testing.T) {
	oldLookup := netLookupHost
	defer func() { netLookupHost = oldLookup }()
	netLookupHost = func(host string) (addrs []string, err error) {
		return nil, errors.New("mock dns error")
	}

	if isAllowedWebhookURL("http://dummy") {
		t.Error("expected false for dns error")
	}

	// Test localhost
	if isAllowedWebhookURL("http://127.0.0.1") {
		t.Error("expected false for localhost")
	}

	// Test invalid scheme
	if isAllowedWebhookURL("ftp://dummy") {
		t.Error("expected false for ftp")
	}
}

func TestDeliverEmailSMTPNewClientError(t *testing.T) {
	oldSMTP := smtpNewClient
	defer func() { smtpNewClient = oldSMTP }()
	smtpNewClient = func(conn net.Conn, host string) (smtpClient, error) {
		return nil, errors.New("mock smtp error")
	}

	err := DeliverEmail("host", 465, "user", "pass", "from", []string{"to"}, NotificationPayload{})
	if err == nil {
		t.Error("expected error for mock smtp error")
	}
}

type mockWriteCloser struct {
	writeErr error
	closeErr error
}

func (m *mockWriteCloser) Write(p []byte) (n int, err error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(p), nil
}

func (m *mockWriteCloser) Close() error {
	return m.closeErr
}

type mockSMTPClient struct {
	authErr error
	mailErr error
	rcptErr error
	dataErr error
	quitErr error
	wc      *mockWriteCloser
}

func (m *mockSMTPClient) Auth(a smtp.Auth) error { return m.authErr }
func (m *mockSMTPClient) Mail(from string) error { return m.mailErr }
func (m *mockSMTPClient) Rcpt(to string) error   { return m.rcptErr }
func (m *mockSMTPClient) Data() (io.WriteCloser, error) {
	if m.dataErr != nil {
		return nil, m.dataErr
	}
	return m.wc, nil
}
func (m *mockSMTPClient) Quit() error  { return m.quitErr }
func (m *mockSMTPClient) Close() error { return nil }

func TestDeliverEmailDetailedErrors(t *testing.T) {
	oldSMTP := smtpNewClient
	defer func() { smtpNewClient = oldSMTP }()

	oldTLS := tlsDial
	defer func() { tlsDial = oldTLS }()
	tlsDial = func(network, addr string, config *tls.Config) (*tls.Conn, error) {
		return tls.Client(&mockConn{}, config), nil
	}

	cases := []struct {
		name   string
		client *mockSMTPClient
	}{
		{"auth err", &mockSMTPClient{authErr: errors.New("err")}},
		{"mail err", &mockSMTPClient{mailErr: errors.New("err")}},
		{"rcpt err", &mockSMTPClient{rcptErr: errors.New("err")}},
		{"data err", &mockSMTPClient{dataErr: errors.New("err")}},
		{"write err", &mockSMTPClient{wc: &mockWriteCloser{writeErr: errors.New("err")}}},
		{"close err", &mockSMTPClient{wc: &mockWriteCloser{closeErr: errors.New("err")}}},
		{"quit err", &mockSMTPClient{wc: &mockWriteCloser{}, quitErr: errors.New("err")}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			smtpNewClient = func(conn net.Conn, host string) (smtpClient, error) {
				return c.client, nil
			}
			DeliverEmail("host", 465, "user", "pass", "from", []string{"to"}, NotificationPayload{})
		})
	}

	// Test empty TO
	smtpNewClient = func(conn net.Conn, host string) (smtpClient, error) {
		return &mockSMTPClient{wc: &mockWriteCloser{}}, nil
	}
	DeliverEmail("host", 465, "user", "pass", "from", []string{}, NotificationPayload{})
	DeliverEmail("host", 465, "user", "pass", "from", nil, NotificationPayload{})
}

type mockConn struct{}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (m *mockConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// --- Merged from delivery_extra_test.go ---

// removed TestIsAllowedWebhookURL since it requires real DNS resolution

func TestSlackWebhook(t *testing.T) {
	received := false
	origTransport := httpClient.Transport
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			received = true
			body, _ := io.ReadAll(req.Body)
			var payload slackPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("Failed to parse slack payload: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(nil)),
			}, nil
		},
	}
	defer func() { httpClient.Transport = origTransport }()

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	payload := NotificationPayload{
		RuleName:      "Test Slack Rule",
		ContainerName: "test-container",
		Type:          "event",
		Details:       "This is a test alert",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	configJSON := `{"url":"http://dummy"}`
	err := DeliverNotification("slack", configJSON, payload)
	if err != nil {
		t.Fatalf("DeliverNotification slack failed: %v", err)
	}
	if !received {
		t.Fatal("Test server did not receive slack webhook")
	}

	// test recovery type
	payload.Type = "recovery"
	_ = DeliverNotification("slack", configJSON, payload)

	// test digest type
	payload.Type = "digest"
	_ = DeliverNotification("slack", configJSON, payload)

	// test audit type
	payload.Type = "audit"
	_ = DeliverNotification("slack", configJSON, payload)

	// test default type
	payload.Type = "unknown"
	_ = DeliverNotification("slack", configJSON, payload)

	// Test long details
	payload.Details = string(make([]byte, 1000))
	_ = DeliverNotification("slack", configJSON, payload)
}

func TestMSTeamsWebhook(t *testing.T) {
	received := false
	origTransport := httpClient.Transport
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			received = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(nil)),
			}, nil
		},
	}
	defer func() { httpClient.Transport = origTransport }()

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	payload := NotificationPayload{
		RuleName:      "Test MSTeams Rule",
		ContainerName: "test-container",
		Type:          "event",
		Details:       "This is a test alert",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	configJSON := `{"url":"http://dummy"}`
	err := DeliverNotification("msteams", configJSON, payload)
	if err != nil {
		t.Fatalf("DeliverNotification msteams failed: %v", err)
	}
	if !received {
		t.Fatal("Test server did not receive msteams webhook")
	}

	types := []string{"recovery", "digest", "audit", "unknown"}
	for _, typ := range types {
		payload.Type = typ
		_ = DeliverNotification("msteams", configJSON, payload)
	}
}

func TestGChatWebhook(t *testing.T) {
	received := false
	origTransport := httpClient.Transport
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			received = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(nil)),
			}, nil
		},
	}
	defer func() { httpClient.Transport = origTransport }()

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	payload := NotificationPayload{
		RuleName:      "Test GChat Rule",
		ContainerName: "test-container",
		Type:          "event",
		Details:       "This is a test alert",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	configJSON := `{"url":"http://dummy"}`
	err := DeliverNotification("gchat", configJSON, payload)
	if err != nil {
		t.Fatalf("DeliverNotification gchat failed: %v", err)
	}
	if !received {
		t.Fatal("Test server did not receive gchat webhook")
	}

	types := []string{"recovery", "digest", "audit", "unknown"}
	for _, typ := range types {
		payload.Type = typ
		_ = DeliverNotification("gchat", configJSON, payload)
	}
}

func TestDeliverNotification_Errors(t *testing.T) {
	err := DeliverNotification("slack", `invalid json`, NotificationPayload{})
	if err == nil {
		t.Fatal("Expected error for invalid json")
	}

	err = DeliverNotification("slack", `{}`, NotificationPayload{})
	if err == nil {
		t.Fatal("Expected error for missing url")
	}

	err = DeliverNotification("unknown_type", `{"url":"http://dummy"}`, NotificationPayload{})
	if err == nil {
		t.Fatal("Expected error for unknown channel type")
	}
}

func TestPostJSON_Errors(t *testing.T) {
	SkipSSRFCheck = false // Ensure SSRF check is active for this test
	err := postJSON("http://127.0.0.1/webhook", []byte("{}"))
	if err == nil {
		t.Fatal("Expected error for blocked SSRF URL")
	}
}

func TestDeliverEmail(t *testing.T) {
	// For DeliverEmail, since it tries to connect to an SMTP server,
	// testing it without a real/mock SMTP server will fail at TLS Dial or smtp.Dial.
	// But we can at least test that it handles invalid configurations.
	payload := NotificationPayload{
		Type:          "test",
		ContainerName: "test-container",
	}
	// Try sending to a dummy port, expecting an error about connection refused
	err := DeliverEmail("127.0.0.1", 12345, "user", "pass", "to@example.com", []string{"cc@example.com"}, payload)
	if err == nil {
		t.Fatal("Expected error connecting to dummy SMTP server")
	}

	// Try with empty recipients
	err = DeliverEmail("127.0.0.1", 12345, "user", "pass", "", []string{}, payload)
	if err == nil {
		t.Fatal("Expected error for no recipients")
	}
}

// --- Merged from manager_extra_test.go ---

func setupTestDBExtra(t *testing.T) {
	db.GormDB, _ = gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	db.GormDB.AutoMigrate(&db.AlertRule{}, &db.Stat{}, &db.Setting{}, &db.Team{}, &db.AlertHistory{})
}

func TestAlertManager_NilChecks(t *testing.T) {
	var am *AlertManager
	am.TriggerSystemAlert("test", "test")
	am.TriggerContainerEvent("test", "test", "test")
}

func TestAlertManager_StartStop(t *testing.T) {
	setupTestDBExtra(t)
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				<-req.Context().Done()
				return nil, req.Context().Err()
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))
	am := NewAlertManager(cli)
	am.Start()
	time.Sleep(50 * time.Millisecond)
	am.Stop()
	am.Stop() // Test double stop
}

func TestAlertManager_LoopsCancel(t *testing.T) {
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				<-req.Context().Done()
				return nil, req.Context().Err()
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))

	ctx, cancel := context.WithCancel(context.Background())
	am := &AlertManager{
		cli: cli,
		ctx: ctx,
	}
	cancel() // cancel immediately
	am.syncLogTailersLoop()
	am.checkMetricsLoop()
	am.listenToDockerEvents()
}

func TestAlertManager_RunEventLoop_ChannelClose(t *testing.T) {
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader([]byte{})), // closes immediately
				}, nil
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	am := &AlertManager{
		cli: cli,
		ctx: ctx,
	}
	am.runEventLoop()
}

func TestAlertManager_TriggerAlerts(t *testing.T) {
	cli, _ := client.NewClientWithOpts()
	am := NewAlertManager(cli)
	am.rules = map[int64]*AlertRule{
		1: {
			ID:               1,
			EventTypes:       "start,die",
			ContainerPattern: ".*",
		},
	}

	am.TriggerSystemAlert("start", "system start")
	am.TriggerSystemAlert("unknown", "system unknown")

	am.TriggerContainerEvent("start", "my-container", "container started")
	am.TriggerContainerEvent("unknown", "my-container", "container unknown")

	// Rule with no event types
	am.rules[2] = &AlertRule{
		ID:               2,
		ContainerPattern: ".*",
	}
	am.TriggerSystemAlert("start", "system start")
	am.TriggerContainerEvent("start", "my-container", "container started")

	// Process container event missing ID
	am.processContainerEvent(events.Message{
		Action: "start",
		Actor: events.Actor{
			Attributes: map[string]string{},
			ID:         "1234567890123456", // length > 12
		},
	})
}

func TestAlertManager_TailContainerLogs(t *testing.T) {
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString("01234567hello world\n")),
				}, nil
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))

	ctx, cancel := context.WithCancel(context.Background())
	am := &AlertManager{
		cli: cli,
		ctx: ctx,
		rules: map[int64]*AlertRule{
			1: {
				ID:               1,
				ContainerPattern: ".*",
				LogPattern:       "hello",
				Enabled:          true,
			},
		},
		debounceMu:      sync.Mutex{},
		groupedDebounce: make(map[string]*ContainerDebounceGroup),
	}

	tailCtx, tailCancel := context.WithCancel(context.Background())
	defer tailCancel()
	go am.tailContainerLogs(tailCtx, "cid-123", "test-container")

	// wait a bit for it to process
	time.Sleep(100 * time.Millisecond)
	cancel()
}

func TestAlertManager_EvaluateMetrics(t *testing.T) {
	setupTestDBExtra(t)
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`[]`)),
				}, nil
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	am := &AlertManager{
		cli: cli,
		ctx: ctx,
		rules: map[int64]*AlertRule{
			1: {
				ID:                 1,
				ContainerPattern:   ".*",
				MetricCPUThreshold: 50.0,
				Enabled:            true,
			},
			2: {
				ID:                 2,
				ContainerPattern:   ".*",
				MetricMemThreshold: 50,
				Enabled:            true,
			},
		},
		debounceMu:      sync.Mutex{},
		groupedDebounce: make(map[string]*ContainerDebounceGroup),
		scanThrottleMu:  sync.Mutex{},
		activeScans:     make(map[string]time.Time),
	}

	// Add dummy stats
	db.GormDB.Create(&db.Stat{
		ContainerID: "test-container",
		CPU:         80.0,
		Memory:      80 * 1024 * 1024,
	})

	am.evaluateMetrics()

	activeRules := []*AlertRule{am.rules[1], am.rules[2]}
	am.evaluateStorageMetrics(activeRules)
}

func TestAlertManager_DeliverGroup(t *testing.T) {
	setupTestDBExtra(t)
	// Add a dummy setting with integrations
	db.GormDB.Create(&db.Setting{
		ID:              1,
		SlackWebhookUrl: "http://dummy",
	})

	am := &AlertManager{
		rules: map[int64]*AlertRule{
			1: {
				ID:               1,
				Name:             "Test Rule",
				ContainerPattern: ".*",
			},
		},
	}
	group := &ContainerDebounceGroup{
		Timer: nil,
		Triggers: map[int64]*TriggeredRule{
			1: {
				Rule: &AlertRule{
					ID:          1,
					Name:        "Test Rule",
					EnableSlack: true,
				},
				Count:   5,
				Details: "error log 1 error log 2",
			},
		},
	}
	am.deliverGroup("test-container", []TriggeredRule{*group.Triggers[1]})
	time.Sleep(100 * time.Millisecond)
}

// --- Merged from coverage_extra3_test.go ---

func TestEvaluateStorageMetrics_FullMock(t *testing.T) {
	origDisk := diskUsage
	origMem := memVirtualMemory
	defer func() {
		diskUsage = origDisk
		memVirtualMemory = origMem
	}()

	am := NewAlertManager(nil)
	am.rules = map[int64]*AlertRule{
		1: {
			ID:                     1,
			ContainerPattern:       "system",
			MetricStorageThreshold: 50,
			MetricMemThreshold:     50,
			compiledContainerRe:    regexp.MustCompile("system"),
		},
	}
	rules := []*AlertRule{am.rules[1]}

	// Test 1: Disk error
	diskUsage = func(path string) (*disk.UsageStat, error) {
		return nil, errors.New("disk error")
	}
	am.evaluateStorageMetrics(rules)

	// Test 2: Mem error
	diskUsage = func(path string) (*disk.UsageStat, error) {
		return &disk.UsageStat{UsedPercent: 60}, nil
	}
	memVirtualMemory = func() (*mem.VirtualMemoryStat, error) {
		return nil, errors.New("mem error")
	}
	am.evaluateStorageMetrics(rules)

	// Test 3: Success triggers
	memVirtualMemory = func() (*mem.VirtualMemoryStat, error) {
		return &mem.VirtualMemoryStat{UsedPercent: 60}, nil
	}
	am.evaluateStorageMetrics(rules)

	// Test 4: empty rules
	am.evaluateStorageMetrics([]*AlertRule{})
}

func TestTailContainerLogs_Full(t *testing.T) {
	headerData := make([]byte, 8)
	// stdout stream (1), size 4
	headerData[0] = 1
	binary.BigEndian.PutUint32(headerData[4:8], 5)

	payload := append(headerData, []byte("FAIL\n")...)

	// Add an empty frame
	emptyFrame := make([]byte, 8)
	emptyFrame[0] = 1
	binary.BigEndian.PutUint32(emptyFrame[4:8], 0)

	payload = append(payload, emptyFrame...)

	// Add an incomplete header
	payload = append(payload, []byte{1, 0, 0}...)

	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(payload)),
				}, nil
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))

	am := &AlertManager{
		cli:             cli,
		tailsMu:         sync.Mutex{},
		activeTails:     make(map[string]context.CancelFunc),
		debounceMu:      sync.Mutex{},
		groupedDebounce: make(map[string]*ContainerDebounceGroup),
		rules: map[int64]*AlertRule{
			1: {
				ID:                  1,
				LogPattern:          "FAIL",
				ContainerPattern:    ".*",
				compiledContainerRe: regexp.MustCompile(".*"),
				compiledLogRe:       regexp.MustCompile("FAIL"),
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	am.activeTails["test"] = cancel

	am.tailContainerLogs(ctx, "test", "test")

	// Test header error
	mockHTTPClient2 := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader([]byte{})), // EOF right away
				}, nil
			},
		},
	}
	cli2, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient2))
	am.cli = cli2
	am.activeTails["test"] = func() {}
	am.tailContainerLogs(ctx, "test", "test")

	// Test payload error
	badPayload := make([]byte, 8)
	badPayload[0] = 1
	binary.BigEndian.PutUint32(badPayload[4:8], 100) // Expect 100 bytes but we give 0

	mockHTTPClient3 := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(badPayload)),
				}, nil
			},
		},
	}
	cli3, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient3))
	am.cli = cli3
	am.activeTails["test"] = func() {}
	am.tailContainerLogs(ctx, "test", "test")
}

func TestDeliverEmail_FullMock(t *testing.T) {
	origTLS := tlsDial
	origSMTP := smtpSendMail
	defer func() {
		tlsDial = origTLS
		smtpSendMail = origSMTP
	}()

	payload := NotificationPayload{
		Type:          "test",
		ContainerName: "test",
		RuleName:      "test",
		Details:       "test",
	}

	// Port 587 success
	smtpSendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		return nil
	}
	err := DeliverEmail("127.0.0.1", 587, "user", "pass", "to@example.com", []string{"cc@example.com"}, payload)
	if err != nil {
		t.Fatalf("Expected 587 success, got %v", err)
	}

	// Port 587 without user/pass
	err = DeliverEmail("127.0.0.1", 587, "", "", "to@example.com", nil, payload)
	if err != nil {
		t.Fatalf("Expected 587 success, got %v", err)
	}

	// Port 465 dial error
	tlsDial = func(network, addr string, config *tls.Config) (*tls.Conn, error) {
		return nil, errors.New("dial error")
	}
	err = DeliverEmail("127.0.0.1", 465, "user", "pass", "to@example.com", nil, payload)
	if err == nil {
		t.Fatal("Expected error on tlsDial")
	}
}

func TestEvaluateMetrics_FullMock(t *testing.T) {
	setupTestDBExtra(t)
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				containers := []map[string]interface{}{
					{
						"Id":    "123456789012",
						"Names": []string{"/test-container"},
					},
					{
						"Id":    "098765432109",
						"Names": []string{"/fresh-container"},
					},
				}
				// for the raw API it returns just array of objects for /containers/json
				b2, _ := json.Marshal(containers)
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(b2)),
				}, nil
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))

	am := NewAlertManager(cli)
	am.rules = map[int64]*AlertRule{
		1: {
			ID:                  1,
			ContainerPattern:    ".*",
			MetricCPUThreshold:  50,
			MetricMemThreshold:  50,
			Enabled:             true,
			compiledContainerRe: regexp.MustCompile(".*"),
		},
	}

	// old start (past grace period)
	am.startedAtMu.Lock()
	am.startedAt["test-container"] = time.Now().Add(-5 * time.Minute)
	am.startedAtMu.Unlock()

	// fresh start (within grace period)
	am.startedAtMu.Lock()
	am.startedAt["fresh-container"] = time.Now()
	am.startedAtMu.Unlock()

	db.GormDB.Create(&db.Stat{
		ContainerID: "123456789012",
		CPU:         80.0,
		Memory:      80 * 1024 * 1024,
		Timestamp:   time.Now(),
	})

	db.GormDB.Create(&db.Stat{
		ContainerID: "098765432109",
		CPU:         80.0,
		Memory:      80 * 1024 * 1024,
		Timestamp:   time.Now(),
	})

	am.evaluateMetrics()
	time.Sleep(100 * time.Millisecond) // wait for debounce
}

func TestDeliverNotification_PostJSON(t *testing.T) {
	origPost := postJSONFunc
	defer func() { postJSONFunc = origPost }()

	postJSONFunc = func(url string, body []byte) error {
		return errors.New("post error")
	}

	payload := NotificationPayload{
		Type:          "event",
		ContainerName: "test",
		RuleName:      "test",
		Details:       "test",
	}

	configJSON := `{"url":"http://dummy"}`

	err := DeliverNotification("slack", configJSON, payload)
	if err == nil {
		t.Fatal("Expected error")
	}

	err = DeliverNotification("msteams", configJSON, payload)
	if err == nil {
		t.Fatal("Expected error")
	}

	err = DeliverNotification("gchat", configJSON, payload)
	if err == nil {
		t.Fatal("Expected error")
	}

	err = DeliverNotification("generic_webhook", configJSON, payload)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestPostJSON_Internal(t *testing.T) {
	origTransport := httpClient.Transport
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 500, // Non 2xx
				Body:       io.NopCloser(bytes.NewBuffer(nil)),
			}, nil
		},
	}
	defer func() { httpClient.Transport = origTransport }()

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	err := postJSON("http://dummy", []byte("{}"))
	if err == nil {
		t.Fatal("Expected error on 500 status code")
	}

	// Error on dial
	httpClient.Transport = &dockerMockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial err")
		},
	}
	err = postJSON("http://dummy", []byte("{}"))
	if err == nil {
		t.Fatal("Expected dial error")
	}
}

type dummyAddr struct{}

func (dummyAddr) Network() string { return "tcp" }
func (dummyAddr) String() string  { return "127.0.0.1" }

func TestSyncLogTailers_Errors(t *testing.T) {
	// ContainerList error
	mockHTTPClient := &http.Client{
		Transport: &dockerMockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("err")
			},
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(mockHTTPClient))
	am := NewAlertManager(cli)
	am.rules = map[int64]*AlertRule{
		1: {
			ID:                  1,
			LogPattern:          "test",
			compiledContainerRe: regexp.MustCompile(".*"),
		},
	}
	am.syncLogTailers()
}

func TestTriggerSystemAlertNil(t *testing.T) {
	var am *AlertManager
	am.TriggerSystemAlert("test", "test")
}

func TestCheckCooldownDeliverGroup(t *testing.T) {
	am := NewAlertManager(nil)
	am.rules = map[int64]*AlertRule{
		1: {
			ID:              1,
			CooldownSeconds: 60,
			Name:            "test",
		},
	}

	tr := TriggeredRule{
		Rule:      am.rules[1],
		AlertType: "event",
		Details:   "test",
		Count:     1,
	}

	am.deliverGroup("test", []TriggeredRule{tr})
}

// --- Merged from manager_extra9_test.go ---

type dockerMockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (t *dockerMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.roundTripFunc(req)
}

func setupTestDB(t *testing.T) {
	// dummy so we don't need to rewrite all files right now
	setupTestManager(t)
}

