package cluster

import (
	"bytes"
	"context"
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
		authHeader = requestHeader.Get("Authorization")
		dialCount++
		if dialCount == 1 {
			return nil, errors.New("dial error")
		}
		defer func() { agentRunning = false }()
		mws := &mockSpokeWSConn{
			readMsgs: [][]byte{
				[]byte(`{"type":"command","action":"start","container_id":"c1"}`),
				[]byte(`{"type":"exec_start","exec_id":"e1","container_id":"c1"}`),
				[]byte(`{"type":"exec_input","data":""}`),
			},
			readErrs: []error{nil, nil, nil},
		}
		return mws, nil
	}

	cli := getMockDockerClient()
	go StartSpokeAgent("http://hub", "tok", "node1", cli)

	time.Sleep(300 * time.Millisecond)
	agentRunning = false
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

	spokeWs = &mockSpokeWSConn{
		writeErr: errors.New("write err"),
	}
	PushToHub("test", map[string]string{"foo": "bar"})
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

func TestHandleCommand(t *testing.T) {
	dockerClient = getMockDockerClient()

	handleCommand("start", "c1")
	handleCommand("stop", "c1")
	handleCommand("restart", "c1")
	handleCommand("delete", "c1")

	// scan success
	scanImageFunc = func(ctx context.Context, cli *client.Client, image string) (map[string]interface{}, error) {
		return map[string]interface{}{}, nil
	}
	handleCommand("scan", "c1")
	time.Sleep(50 * time.Millisecond)

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
	handleCommand("scan", "c1")
	time.Sleep(50 * time.Millisecond)
	dockerClient = origCli

	// scan error in scanner
	scanImageFunc = func(ctx context.Context, cli *client.Client, image string) (map[string]interface{}, error) {
		return map[string]interface{}{}, errors.New("scan err")
	}
	handleCommand("scan", "c1")
	time.Sleep(50 * time.Millisecond)
}

func TestHandleExecSession(t *testing.T) {
	handleExecSession("e1", "c1")
}
