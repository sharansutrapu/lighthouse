package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/smtp"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"lighthouse/db"
)

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
