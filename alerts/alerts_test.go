package alerts

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"lighthouse/db"
	"lighthouse/scanner"

	"context"
	"github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
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
