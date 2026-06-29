package alerts

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lighthouse/db"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestManager(t *testing.T) *AlertManager {
	// Initialize a fresh in-memory SQLite for DB dependencies
	var err error
	db.GormDB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	db.GormDB.AutoMigrate(&db.AlertRule{}, &db.AlertHistory{}, &db.Setting{})
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

	// First trigger should pass
	if !am.checkCooldown(ruleID, cooldown) {
		t.Fatal("Expected checkCooldown to be true on first call")
	}

	// Immediate second trigger should fail
	if am.checkCooldown(ruleID, cooldown) {
		t.Fatal("Expected checkCooldown to be false immediately after first call")
	}

	// Wait for cooldown
	time.Sleep(3 * time.Second)

	// Third trigger should pass
	if !am.checkCooldown(ruleID, cooldown) {
		t.Fatal("Expected checkCooldown to be true after cooldown expires")
	}
}

func TestWebhookDelivery(t *testing.T) {
	received := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		body, _ := io.ReadAll(r.Body)
		var payload NotificationPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("Failed to parse payload: %v", err)
		}
		if payload.RuleName != "Test Webhook Rule" {
			t.Errorf("Expected rule name 'Test Webhook Rule', got %s", payload.RuleName)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	payload := NotificationPayload{
		RuleName:      "Test Webhook Rule",
		ContainerName: "test-container",
		Type:          "test",
		Details:       "This is a test alert",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	SkipSSRFCheck = true
	defer func() { SkipSSRFCheck = false }()

	configJSON := `{"url":"` + ts.URL + `"}`
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
