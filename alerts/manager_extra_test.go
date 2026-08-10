package alerts

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"lighthouse/db"

	"github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"gorm.io/gorm"
)

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
