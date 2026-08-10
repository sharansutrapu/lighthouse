package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"lighthouse/db"

	"github.com/moby/moby/client"
)

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
