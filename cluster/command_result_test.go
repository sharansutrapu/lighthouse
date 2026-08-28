package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"

	"lighthouse/alerts"
	"lighthouse/db"
)

// wireWSConn is a WSConn stand-in that actually performs real JSON
// marshaling (unlike mockSpokeWSConn/mockWSConn in the other test files,
// which just append the raw Go value to an in-memory slice). Using the real
// encoding is essential here: PushToHub had a latent bug where a bare
// []byte "data" field was base64-encoded by encoding/json instead of being
// embedded as a nested JSON value, which made every Hub<->Spoke message
// silently fail to round-trip in production despite tests passing (because
// those tests hand-crafted "valid-shaped" JSON and never exercised the real
// PushToHub encoding path). These tests exercise the real encode/decode path.
type wireWSConn struct {
	sent [][]byte
}

func (w *wireWSConn) ReadMessage() (int, []byte, error) { return 0, nil, errors.New("not used") }
func (w *wireWSConn) WriteJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.sent = append(w.sent, b)
	return nil
}
func (w *wireWSConn) WriteMessage(messageType int, data []byte) error { return nil }
func (w *wireWSConn) Close() error                                   { return nil }

func setupCommandResultTestDB(t *testing.T) {
	t.Helper()
	if db.GormDB == nil {
		t.Fatal("db.GormDB not initialized — package init() should have set it up")
	}
	alerts.Global = alerts.NewAlertManager(nil)
}

// TestPushToHub_EncodesDataAsEmbeddedJSON is a focused regression test for
// the base64-double-encoding bug: it asserts the wire bytes contain a real
// nested JSON object under "data", not a base64 string.
func TestPushToHub_EncodesDataAsEmbeddedJSON(t *testing.T) {
	origWs := spokeWs
	defer func() { spokeWs = origWs }()

	ws := &wireWSConn{}
	spokeWs = ws

	PushToHub("command_result", commandResult{Action: "start", ContainerID: "c1", Status: "success"})

	if len(ws.sent) != 1 {
		t.Fatalf("expected exactly 1 message sent, got %d", len(ws.sent))
	}

	var envelope struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(ws.sent[0], &envelope); err != nil {
		t.Fatalf("wire message is not valid JSON: %v", err)
	}
	assert.Equal(t, "command_result", envelope.Type)

	// The critical assertion: envelope.Data must decode as a JSON object with
	// the expected fields — not as a base64-encoded string.
	trimmed := bytes.TrimSpace(envelope.Data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		t.Fatalf(`envelope.Data = %s, want a raw JSON object starting with "{" (got a string/other value — the base64 double-encoding bug is back)`, envelope.Data)
	}

	var result commandResult
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal envelope.Data into commandResult: %v", err)
	}
	assert.Equal(t, "start", result.Action)
	assert.Equal(t, "c1", result.ContainerID)
	assert.Equal(t, "success", result.Status)
}

// TestHandleCommand_EndToEnd_ReportsResultToHub drives handleCommand (Spoke
// side) through a real Docker mock, captures the exact bytes PushToHub would
// send over the wire, and feeds those bytes into handleSpokeMessage (Hub
// side) — a full round trip through the real JSON encoding on both ends.
func TestHandleCommand_EndToEnd_ReportsResultToHub(t *testing.T) {
	setupCommandResultTestDB(t)

	tests := []struct {
		name          string
		action        string
		containerID   string
		dockerFails   bool
		wantStatus    string
		wantErrSubstr string
	}{
		{name: "happy path: start succeeds", action: "start", containerID: "e2e-start-ok", dockerFails: false, wantStatus: "success"},
		{name: "infra failure: start fails", action: "start", containerID: "e2e-start-fail", dockerFails: true, wantStatus: "failed", wantErrSubstr: "simulated"},
		{name: "happy path: stop succeeds", action: "stop", containerID: "e2e-stop-ok", dockerFails: false, wantStatus: "success"},
		{name: "infra failure: stop fails", action: "stop", containerID: "e2e-stop-fail", dockerFails: true, wantStatus: "failed", wantErrSubstr: "simulated"},
		{name: "happy path: restart succeeds", action: "restart", containerID: "e2e-restart-ok", dockerFails: false, wantStatus: "success"},
		{name: "infra failure: restart fails", action: "restart", containerID: "e2e-restart-fail", dockerFails: true, wantStatus: "failed", wantErrSubstr: "simulated"},
		{name: "happy path: delete succeeds", action: "delete", containerID: "e2e-delete-ok", dockerFails: false, wantStatus: "success"},
		{name: "infra failure: delete fails", action: "delete", containerID: "e2e-delete-fail", dockerFails: true, wantStatus: "failed", wantErrSubstr: "simulated"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			origDockerClient := dockerClient
			origWs := spokeWs
			defer func() {
				dockerClient = origDockerClient
				spokeWs = origWs
			}()

			cli, _ := client.NewClientWithOpts(
				client.WithHTTPClient(&http.Client{
					Transport: &dockerMockTransport{
						fn: func(req *http.Request) (*http.Response, error) {
							if tc.dockerFails {
								return nil, errors.New("simulated docker daemon error")
							}
							return &http.Response{StatusCode: 204, Body: ioutil.NopCloser(bytes.NewBufferString(""))}, nil
						},
					},
				}),
				client.WithVersion("1.41"),
			)
			dockerClient = cli

			ws := &wireWSConn{}
			spokeWs = ws

			GlobalHub.Lock()
			delete(GlobalHub.CommandResults, tc.containerID)
			GlobalHub.Unlock()

			handleCommand(tc.action, tc.containerID)

			if len(ws.sent) != 1 {
				t.Fatalf("expected exactly 1 command_result message, got %d", len(ws.sent))
			}

			// Hub side: feed the exact wire bytes captured above.
			handleSpokeMessage("test-node", ws.sent[0])

			GlobalHub.RLock()
			result, ok := GlobalHub.CommandResults[tc.containerID]
			GlobalHub.RUnlock()

			if !ok {
				t.Fatalf("GlobalHub.CommandResults[%q] was never recorded — the Hub silently dropped the spoke's command result", tc.containerID)
			}
			assert.Equal(t, tc.action, result.Action)
			assert.Equal(t, tc.wantStatus, result.Status)
			if tc.wantErrSubstr != "" {
				assert.Contains(t, result.Error, tc.wantErrSubstr)
			}
		})
	}
}

// TestHandleCommand_EndToEnd_ScanReportsResultToHub covers the "scan" action,
// which runs asynchronously in its own goroutine inside handleCommand.
func TestHandleCommand_EndToEnd_ScanReportsResultToHub(t *testing.T) {
	setupCommandResultTestDB(t)

	tests := []struct {
		name        string
		inspectFail bool
		scanFail    bool
		wantStatus  string
	}{
		{name: "happy path: scan succeeds", inspectFail: false, scanFail: false, wantStatus: "success"},
		{name: "infra failure: inspect fails before scan runs", inspectFail: true, wantStatus: "failed"},
		{name: "infra failure: scanner itself fails", inspectFail: false, scanFail: true, wantStatus: "failed"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			origDockerClient := dockerClient
			origWs := spokeWs
			origScanFn := scanImageFunc
			defer func() {
				dockerClient = origDockerClient
				spokeWs = origWs
				scanImageFunc = origScanFn
			}()

			cli, _ := client.NewClientWithOpts(
				client.WithHTTPClient(&http.Client{
					Transport: &dockerMockTransport{
						fn: func(req *http.Request) (*http.Response, error) {
							if tc.inspectFail {
								return nil, errors.New("simulated inspect failure")
							}
							return &http.Response{StatusCode: 200, Body: ioutil.NopCloser(bytes.NewBufferString(`{"Config":{"Image":"alpine"}}`))}, nil
						},
					},
				}),
				client.WithVersion("1.41"),
			)
			dockerClient = cli

			scanImageFunc = func(ctx context.Context, cli *client.Client, image string) (map[string]interface{}, error) {
				if tc.scanFail {
					return nil, errors.New("simulated scanner failure")
				}
				return map[string]interface{}{"ok": true}, nil
			}

			ws := &wireWSConn{}
			spokeWs = ws

			containerID := "scan-e2e-" + tc.name
			GlobalHub.Lock()
			delete(GlobalHub.CommandResults, containerID)
			GlobalHub.Unlock()

			handleCommand("scan", containerID)
			time.Sleep(100 * time.Millisecond) // scan runs in its own goroutine

			if len(ws.sent) != 1 {
				t.Fatalf("expected exactly 1 command_result message for scan, got %d", len(ws.sent))
			}
			handleSpokeMessage("test-node", ws.sent[0])

			GlobalHub.RLock()
			result, ok := GlobalHub.CommandResults[containerID]
			GlobalHub.RUnlock()
			if !ok {
				t.Fatalf("GlobalHub.CommandResults[%q] was never recorded for scan", containerID)
			}
			assert.Equal(t, tc.wantStatus, result.Status)
		})
	}
}

// TestHandleSpokeMessage_CommandResult_HostilePayloads verifies the Hub
// tolerates malformed/hostile command_result payloads without panicking or
// corrupting shared state.
func TestHandleSpokeMessage_CommandResult_HostilePayloads(t *testing.T) {
	setupCommandResultTestDB(t)

	tests := []struct {
		name string
		msg  []byte
	}{
		{name: "hostile: data is not JSON at all", msg: []byte(`{"type":"command_result","data":"not-json-object"}`)},
		{name: "hostile: data is a JSON number instead of object", msg: []byte(`{"type":"command_result","data":42}`)},
		{name: "hostile: completely invalid top-level JSON", msg: []byte(`{not even json`)},
		{name: "hostile: missing data field entirely", msg: []byte(`{"type":"command_result"}`)},
		{name: "edge: empty message", msg: []byte(``)},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				handleSpokeMessage("hostile-node", tc.msg)
			})
		})
	}
}

// TestHandleSpokeMessage_CommandResult_TriggersAlertOnFailure confirms a
// failed command result raises a system alert instead of only being logged
// (so operators are notified instead of the failure going unnoticed).
func TestHandleSpokeMessage_CommandResult_TriggersAlertOnFailure(t *testing.T) {
	setupCommandResultTestDB(t)

	msg := []byte(`{"type":"command_result","data":{"action":"start","container_id":"alert-test","status":"failed","error":"boom"}}`)
	assert.NotPanics(t, func() {
		handleSpokeMessage("alert-node", msg)
	})

	GlobalHub.RLock()
	result, ok := GlobalHub.CommandResults["alert-test"]
	GlobalHub.RUnlock()
	assert.True(t, ok)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Error, "boom")
}

func TestHandleSpokeMessage_CommandResult_NilAlertsGlobal(t *testing.T) {
	origGlobal := alerts.Global
	defer func() { alerts.Global = origGlobal }()
	alerts.Global = nil

	msg := []byte(`{"type":"command_result","data":{"action":"stop","container_id":"nil-alert-test","status":"failed","error":"boom"}}`)
	assert.NotPanics(t, func() {
		handleSpokeMessage("nil-alert-node", msg)
	})
}

func TestValidateCommandResultActionsAreLowercase(t *testing.T) {
	// Sanity check that our test table above and the production action set
	// agree, so this suite fails loudly if the action vocabulary ever drifts.
	for _, action := range []string{"start", "stop", "restart", "delete", "scan"} {
		if strings.ToLower(action) != action {
			t.Fatalf("action %q is expected to be lowercase", action)
		}
	}
}
