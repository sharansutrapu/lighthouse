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

	"github.com/glebarez/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"lighthouse/db"
)

func init() {
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	d.AutoMigrate(&db.ImageScanResult{})
	db.GormDB = d
}

type mockSpokeWSConn struct {
	readMsgs [][]byte
	readErrs []error
	readIdx  int
	writes   []interface{}
	writeErr error
	closed   bool
}

func (m *mockSpokeWSConn) ReadMessage() (int, []byte, error) {
	if m.readIdx >= len(m.readMsgs) {
		time.Sleep(20 * time.Millisecond) // allow sync loop to trigger
		return 0, nil, errors.New("closed")
	}
	msg := m.readMsgs[m.readIdx]
	err := m.readErrs[m.readIdx]
	m.readIdx++
	return 1, msg, err
}
func (m *mockSpokeWSConn) WriteJSON(v interface{}) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.writes = append(m.writes, v)
	return nil
}
func (m *mockSpokeWSConn) WriteMessage(messageType int, data []byte) error {
	return nil
}
func (m *mockSpokeWSConn) Close() error {
	m.closed = true
	return nil
}

type dockerMockTransport struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *dockerMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func getMockDockerClient() *client.Client {
	cli, _ := client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{
			Transport: &dockerMockTransport{
				fn: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Path, "/json") && req.Method == "GET" && !strings.Contains(req.URL.Path, "/containers/json") {
						return &http.Response{
							StatusCode: 200,
							Body:       ioutil.NopCloser(bytes.NewBufferString(`{"Config":{"Image":"ubuntu"}}`)),
						}, nil
					}
					if strings.Contains(req.URL.Path, "/containers/json") {
						return &http.Response{
							StatusCode: 200,
							Body:       ioutil.NopCloser(bytes.NewBufferString(`[{"Id":"c1"}]`)),
						}, nil
					}
					return &http.Response{
						StatusCode: 204,
						Body:       ioutil.NopCloser(bytes.NewBufferString("")),
					}, nil
				},
			},
		}),
		client.WithVersion("1.41"),
	)
	return cli
}

func TestSpokeConnectURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"http", "http://hub:8000", "ws://hub:8000/api/spoke/connect?node_id=node+1%26east"},
		{"https", "https://hub/", "wss://hub/api/spoke/connect?node_id=node+1%26east"},
		{"ws", "ws://hub", "ws://hub/api/spoke/connect?node_id=node+1%26east"},
		{"wss prefix", "wss://hub/lighthouse/", "wss://hub/lighthouse/api/spoke/connect?node_id=node+1%26east"},
		{"endpoint", "https://hub/api/spoke/connect", "wss://hub/api/spoke/connect?node_id=node+1%26east"},
		{"query", "https://hub/?region=east&token=obsolete", "wss://hub/api/spoke/connect?node_id=node+1%26east&region=east"},
		{"missing scheme", "hub:8000", ""},
		{"unsupported scheme", "ftp://hub", ""},
		{"missing host", "https:///", ""},
		{"credentials", "https://user:password@hub", ""},
		{"fragment", "https://hub/#settings", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := spokeConnectURL(test.input, "node 1&east")
			if test.want == "" {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, test.want, actual)
		})
	}
}

func TestStartSpokeAgent(t *testing.T) {
	originalDialFunc := dialFunc
	originalSyncInterval := syncInterval
	originalReconnectInterval := reconnectInterval
	originalAgentRunning := agentRunning
	t.Cleanup(func() {
		dialFunc = originalDialFunc
		syncInterval = originalSyncInterval
		reconnectInterval = originalReconnectInterval
		agentRunning = originalAgentRunning
	})

	// Call default dialFunc to cover it
	_, _ = dialFunc("http://invalid", nil)

	syncInterval = 10 * time.Millisecond
	reconnectInterval = 10 * time.Millisecond
	agentRunning = true

	dialCount := 0
	var authHeader string
	dialFunc = func(url string, requestHeader http.Header) (WSConn, error) {
		assert.Equal(t, "ws://hub/api/spoke/connect?node_id=node1", url)
		authHeader = requestHeader.Get("Authorization")
		dialCount++
		if dialCount == 1 {
			return nil, errors.New("dial error")
		}
		defer func() { agentRunning = false }()
		mws := &mockSpokeWSConn{
			readMsgs: [][]byte{
				[]byte(`{"type":"command","action":"start","container_id":"c1"}`),
			},
			readErrs: []error{nil},
		}
		return mws, nil
	}

	cli := getMockDockerClient()
	StartSpokeAgent("http://hub", "tok", "node1", cli)
	assert.GreaterOrEqual(t, dialCount, 1)
	assert.Equal(t, "Bearer tok", authHeader)
}

func TestPushToHub(t *testing.T) {
	spokeWs = nil
	PushToHub("test", map[string]string{"foo": "bar"})

	mws := &mockSpokeWSConn{}
	spokeWs = mws
	PushToHub("test", map[string]string{"foo": "bar"})
	assert.Len(t, mws.writes, 1)

	PushToHub("test", make(chan int))

	failedWS := &mockSpokeWSConn{
		writeErr: errors.New("write err"),
	}
	spokeWs = failedWS
	PushToHub("test", map[string]string{"foo": "bar"})
	assert.True(t, failedWS.closed)
	assert.Nil(t, spokeWs)
}

func TestHandleHubMessage(t *testing.T) {
	dockerClient = getMockDockerClient()
	handleHubMessage([]byte(`invalid json`))
	handleHubMessage([]byte(`{"type":"command","action":"invalid","container_id":"c1"}`))

	ctx, cancel := context.WithCancel(context.Background())
	activeLogStreams.Lock()
	activeLogStreams.cancels["logs1"] = cancel
	activeLogStreams.Unlock()
	handleHubMessage([]byte(`{"type":"log_stop","stream_id":"logs1"}`))
	assert.Error(t, ctx.Err())
}

func TestHubLogWriter(t *testing.T) {
	mws := &mockSpokeWSConn{}
	spokeWriteMu.Lock()
	spokeWs = mws
	spokeWriteMu.Unlock()
	t.Cleanup(func() {
		spokeWriteMu.Lock()
		spokeWs = nil
		spokeWriteMu.Unlock()
	})

	writer := &hubLogWriter{streamID: "logs1"}
	written, err := writer.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, written)
	if assert.Len(t, mws.writes, 1) {
		payload := mws.writes[0].(map[string]interface{})
		assert.Equal(t, "log_output", payload["type"])
	}
}

func TestHandleRPCRequest(t *testing.T) {
	dockerClient = getMockDockerClient()
	mws := &mockSpokeWSConn{}
	spokeWriteMu.Lock()
	spokeWs = mws
	spokeWriteMu.Unlock()
	t.Cleanup(func() {
		spokeWriteMu.Lock()
		spokeWs = nil
		spokeWriteMu.Unlock()
	})

	request := json.RawMessage(`{"container_id":"c1","action":"start"}`)
	handleRPCRequest("request1", "container_action", request)
	if assert.Len(t, mws.writes, 1) {
		payload := mws.writes[0].(map[string]interface{})
		assert.Equal(t, "response", payload["type"])
		encoded, err := json.Marshal(payload["data"])
		assert.NoError(t, err)
		assert.Contains(t, string(encoded), "request1")
	}
}

func TestRPCDispatchAllActionsAndErrors(t *testing.T) {
	originalClient, originalWS := dockerClient, spokeWs
	t.Cleanup(func() { dockerClient, spokeWs = originalClient, originalWS })
	tests := []struct {
		action string
		data   string
		body   string
	}{
		{"container_inspect", `{"container_id":"c1"}`, `{"Id":"c1","Config":{"Image":"alpine"}}`},
		{"container_action", `{"container_id":"c1","action":"start"}`, `{}`},
		{"container_action", `{"container_id":"c1","action":"stop"}`, `{}`},
		{"container_action", `{"container_id":"c1","action":"restart"}`, `{}`},
		{"container_action", `{"container_id":"c1","action":"remove"}`, `{}`},
		{"list_images", `null`, `[]`},
		{"list_volumes", `null`, `{"Volumes":[]}`},
		{"list_networks", `null`, `[]`},
		{"image_remove", `{"id":"image"}`, `[]`},
		{"volume_remove", `{"name":"volume"}`, `{}`},
		{"network_remove", `{"id":"network"}`, `{}`},
		{"image_prune", `{"all_unused":true,"remove_containers":true}`, `{"ImagesDeleted":[],"SpaceReclaimed":7}`},
		{"image_prune", `{"all_unused":false,"remove_containers":false}`, `{"ImagesDeleted":[],"SpaceReclaimed":7}`},
		{"volume_prune", `{"remove_containers":true}`, `{"VolumesDeleted":[],"SpaceReclaimed":7}`},
		{"network_prune", `{"remove_containers":false}`, `{"NetworksDeleted":[]}`},
	}
	for _, test := range tests {
		for _, failDocker := range []bool{false, true} {
			name := test.action + test.data
			if failDocker {
				name += " Docker error"
			}
			t.Run(name, func(t *testing.T) {
				connection := &wireWSConn{}
				spokeWs = connection
				var err error
				dockerClient, err = client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHTTPClient(&http.Client{
					Transport: &dockerMockTransport{fn: func(request *http.Request) (*http.Response, error) {
						if failDocker {
							return nil, errors.New("fixture Docker failure")
						}
						body := test.body
						if strings.Contains(request.URL.Path, "/containers/json") {
							body = `[{"Id":"stopped"}]`
						}
						if strings.Contains(request.URL.Path, "/containers/prune") {
							body = `{"ContainersDeleted":[],"SpaceReclaimed":0}`
						}
						return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: ioutil.NopCloser(strings.NewReader(body))}, nil
					}},
				}))
				assert.NoError(t, err)
				handleRPCRequest("correlation-id", test.action, json.RawMessage(test.data))
				if !assert.Len(t, connection.sent, 1) {
					return
				}
				var envelope struct {
					Type string      `json:"type"`
					Data RPCResponse `json:"data"`
				}
				assert.NoError(t, json.Unmarshal(connection.sent[0], &envelope))
				assert.Equal(t, "response", envelope.Type)
				assert.Equal(t, "correlation-id", envelope.Data.RequestID)
				if failDocker {
					assert.Contains(t, envelope.Data.Error, "fixture Docker failure")
				} else {
					assert.Empty(t, envelope.Data.Error)
					assert.NotEmpty(t, envelope.Data.Data)
				}
			})
		}
	}
	for _, action := range []string{"container_inspect", "container_action", "image_remove", "volume_remove", "network_remove", "unsupported"} {
		connection := &wireWSConn{}
		spokeWs = connection
		handleRPCRequest("malformed-id", action, json.RawMessage(`"not an object"`))
		if !assert.Len(t, connection.sent, 1) {
			continue
		}
		var envelope struct {
			Data RPCResponse `json:"data"`
		}
		assert.NoError(t, json.Unmarshal(connection.sent[0], &envelope))
		assert.NotEmpty(t, envelope.Data.Error)
	}
}

func TestHandleCommand(t *testing.T) {
	originalClient := dockerClient
	originalWS := spokeWs
	originalScan := scanImageFunc
	t.Cleanup(func() {
		dockerClient = originalClient
		spokeWs = originalWS
		scanImageFunc = originalScan
	})
	dockerClient = getMockDockerClient()

	runScan := func() {
		t.Helper()
		done := make(chan struct{})
		spokeWs = &wireWSConn{onWrite: func(message []byte) {
			var envelope struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(message, &envelope) == nil && envelope.Type == "command_result" {
				close(done)
			}
		}}
		handleCommand("scan", "c1")
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("scan command did not complete")
		}
	}

	handleCommand("start", "c1")
	handleCommand("stop", "c1")
	handleCommand("restart", "c1")
	handleCommand("delete", "c1")

	// scan success
	scanImageFunc = func(ctx context.Context, cli *client.Client, image string) (map[string]interface{}, error) {
		return map[string]interface{}{}, nil
	}
	runScan()

	// scan error in inspect
	origCli := dockerClient
	dockerClient, _ = client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{
			Transport: &dockerMockTransport{
				fn: func(req *http.Request) (*http.Response, error) {
					return nil, errors.New("inspect err")
				},
			},
		}),
		client.WithVersion("1.41"),
	)
	runScan()
	dockerClient = origCli

	// scan error in scanner
	scanImageFunc = func(ctx context.Context, cli *client.Client, image string) (map[string]interface{}, error) {
		return map[string]interface{}{}, errors.New("scan err")
	}
	runScan()
}

func TestHandleExecSession(t *testing.T) {
	originalWS := spokeWs
	mws := &mockSpokeWSConn{}
	spokeWs = mws
	t.Cleanup(func() { spokeWs = originalWS })

	handleExecSession("e1", "c1", "/usr/bin/zsh")
	if assert.Len(t, mws.writes, 2) {
		assert.Equal(t, "exec_error", mws.writes[0].(map[string]interface{})["type"])
		assert.Equal(t, "exec_end", mws.writes[1].(map[string]interface{})["type"])
	}
}

type mockExecConnection struct {
	bytes.Buffer
	closed bool
}

func (conn *mockExecConnection) Close() error {
	conn.closed = true
	return nil
}

func TestExecInputAndCleanup(t *testing.T) {
	activeExecSessions.Lock()
	activeExecSessions.sessions = make(map[string]activeExecSession)
	activeExecSessions.pending = make(map[string][][]byte)
	activeExecSessions.Unlock()
	t.Cleanup(cancelAllExecSessions)

	writeExecInput("pending", []byte("first"))
	activeExecSessions.Lock()
	assert.Equal(t, []byte("first"), activeExecSessions.pending["pending"][0])
	activeExecSessions.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	conn := &mockExecConnection{}
	activeExecSessions.Lock()
	activeExecSessions.sessions["active"] = activeExecSession{conn: conn, cancel: cancel}
	activeExecSessions.Unlock()
	writeExecInput("active", []byte("echo test\n"))
	assert.Equal(t, "echo test\n", conn.String())

	stopExecSession("active")
	assert.True(t, conn.closed)
	assert.Error(t, ctx.Err())
}
