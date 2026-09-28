package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
	"lighthouse/db"
)

func init() {
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	d.AutoMigrate(&db.Stat{}, &db.SystemStat{})
	db.GormDB = d
}

type mockWSConn struct {
	readMsgs [][]byte
	readErrs []error
	readIdx  int
	writes   []interface{}
	onWrite  func(interface{})
}

func (m *mockWSConn) ReadMessage() (int, []byte, error) {
	if m.readIdx >= len(m.readMsgs) {
		return 0, nil, errors.New("closed")
	}
	msg := m.readMsgs[m.readIdx]
	err := m.readErrs[m.readIdx]
	m.readIdx++
	return 1, msg, err
}
func (m *mockWSConn) WriteJSON(v interface{}) error {
	m.writes = append(m.writes, v)
	if m.onWrite != nil {
		m.onWrite(v)
	}
	return nil
}
func (m *mockWSConn) WriteMessage(messageType int, data []byte) error {
	m.writes = append(m.writes, data)
	return nil
}
func (m *mockWSConn) Close() error {
	return nil
}

func TestRegisterHubRoutes(t *testing.T) {
	e := echo.New()
	originalUpgrader := upgraderFunc
	t.Cleanup(func() { upgraderFunc = originalUpgrader })

	// Call default upgraderFunc to cover it, including the CheckOrigin closure
	// (only invoked by gorilla/websocket when the request actually looks like
	// a WS handshake with an Origin header present).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	_, _ = upgraderFunc(rec, req) // still errors (ResponseRecorder isn't a Hijacker), but covers CheckOrigin

	RegisterHubRoutes(e, "secret")

	// Invalid token
	req = httptest.NewRequest(http.MethodGet, "/api/spoke/connect?token=wrong", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Missing node_id
	req = httptest.NewRequest(http.MethodGet, "/api/spoke/connect", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Upgrader error
	upgraderFunc = func(w http.ResponseWriter, r *http.Request) (WSConn, error) {
		return nil, errors.New("upgrade error")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/spoke/connect?node_id=node1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	// it should log or return err, echo returns 500 when handler returns err
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// Success and disconnection
	mws := &mockWSConn{
		readMsgs: [][]byte{[]byte(`{"type":"invalid"}`)},
		readErrs: []error{nil},
	}
	upgraderFunc = func(w http.ResponseWriter, r *http.Request) (WSConn, error) {
		return mws, nil
	}
	req = httptest.NewRequest(http.MethodGet, "/api/spoke/connect?node_id=node2", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	GlobalHub.RLock()
	_, exists := GlobalHub.Spokes["node2"]
	GlobalHub.RUnlock()
	assert.False(t, exists, "Spoke should be removed after disconnect")
}

func TestHandleSpokeMessage(t *testing.T) {
	originalDB := db.GormDB
	testDB, err := gorm.Open(sqlite.Open("file:hub_message_test?mode=memory&cache=shared"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, testDB.AutoMigrate(&db.Stat{}, &db.SystemStat{}))
	db.GormDB = testDB
	t.Cleanup(func() { db.GormDB = originalDB })

	// containers
	GlobalHub.Lock()
	GlobalHub.SpokeContainers["node1"] = nil
	GlobalHub.Unlock()

	msg := []byte(`{"type":"containers","data":[{"id":"c1"}]}`)
	handleSpokeMessage("node1", msg)

	GlobalHub.RLock()
	containers := GlobalHub.SpokeContainers["node1"]
	GlobalHub.RUnlock()
	assert.Len(t, containers, 1)

	// stat
	msg = []byte(`{"type":"stat","data":{"cpu_percent": 10.5}}`)
	handleSpokeMessage("node1", msg)

	// The spoke publishes container_stat; keep the legacy stat alias for
	// compatibility while ensuring the production message is persisted.
	msg = []byte(`{"type":"container_stat","data":{"container_id":"c1","cpu":10.5}}`)
	handleSpokeMessage("node1", msg)
	var stat db.Stat
	assert.NoError(t, db.GormDB.Where("node_id = ? AND container_id = ?", "node1", "c1").Last(&stat).Error)
	assert.Equal(t, 10.5, stat.CPU)

	// system_stat
	msg = []byte(`{"type":"system_stat","data":{"cpu": 20.5}}`)
	handleSpokeMessage("node1", msg)

	// exec_output
	uiWs := &mockWSConn{}
	RegisterExecStream("exec1", uiWs)
	msg = []byte(`{"type":"exec_output","data":{"exec_id":"exec1","data":"hello"}}`)
	handleSpokeMessage("node1", msg)
	assert.Len(t, uiWs.writes, 1)
	UnregisterExecStream("exec1")

	// remote log output
	logWs := &mockWSConn{}
	RegisterLogStream("logs1", logWs)
	msg = []byte(`{"type":"log_output","data":{"stream_id":"logs1","data":"hello logs"}}`)
	handleSpokeMessage("node1", msg)
	assert.Len(t, logWs.writes, 1)
	assert.Equal(t, []byte("hello logs"), logWs.writes[0])
	UnregisterLogStream("logs1")

	// invalid payload
	handleSpokeMessage("node1", []byte(`invalid json`))
}

func TestSendCommandToSpoke(t *testing.T) {
	// Not connected
	err := SendCommandToSpoke("node_not_found", "start", "c1")
	assert.Error(t, err)

	// Connected
	mws := &mockWSConn{}
	GlobalHub.Lock()
	GlobalHub.Spokes["node_cmd"] = mws
	GlobalHub.Unlock()

	err = SendCommandToSpoke("node_cmd", "start", "c1")
	assert.NoError(t, err)
	assert.Len(t, mws.writes, 1)
}

func TestRemoteLogRouting(t *testing.T) {
	mws := &mockWSConn{}
	GlobalHub.Lock()
	GlobalHub.Spokes["logs-node"] = mws
	GlobalHub.SpokeContainers["logs-node"] = []map[string]interface{}{
		{"ID": "abcdef1234567890", "Names": []interface{}{`/remote-api`}, "Image": "api:latest"},
	}
	GlobalHub.Unlock()
	t.Cleanup(func() {
		GlobalHub.Lock()
		delete(GlobalHub.Spokes, "logs-node")
		delete(GlobalHub.SpokeContainers, "logs-node")
		GlobalHub.Unlock()
	})

	nodeID, name, image, found := FindSpokeContainer("abcdef123456")
	assert.True(t, found)
	assert.Equal(t, "logs-node", nodeID)
	assert.Equal(t, "remote-api", name)
	assert.Equal(t, "api:latest", image)

	assert.NoError(t, SendLogStart(nodeID, "stream1", "abcdef123456"))
	assert.NoError(t, SendLogStop(nodeID, "stream1"))
	assert.Len(t, mws.writes, 2)
}

func TestSpokeCapabilityNegotiation(t *testing.T) {
	GlobalHub.Lock()
	GlobalHub.Spokes["cap-node"] = &mockWSConn{}
	delete(GlobalHub.SpokeCapabilities, "cap-node")
	GlobalHub.Unlock()
	t.Cleanup(func() {
		GlobalHub.Lock()
		delete(GlobalHub.Spokes, "cap-node")
		delete(GlobalHub.SpokeCapabilities, "cap-node")
		GlobalHub.Unlock()
	})

	assert.True(t, SpokeSupports("cap-node", "logs"))
	assert.False(t, SpokeSupports("cap-node", "shell"))
	assert.NotContains(t, ConnectedSpokeIDsWithCapability("resources"), "cap-node")

	handleSpokeMessage("cap-node", []byte(`{"type":"capabilities","data":{"protocol_version":1,"capabilities":["logs","shell","resources"]}}`))
	assert.True(t, SpokeSupports("cap-node", "shell"))
	assert.Contains(t, ConnectedSpokeIDsWithCapability("resources"), "cap-node")
}

func TestCallSpoke(t *testing.T) {
	mws := &mockWSConn{}
	mws.onWrite = func(value interface{}) {
		payload := value.(map[string]interface{})
		requestID := payload["request_id"].(string)
		responseData, _ := json.Marshal(map[string]string{"status": "ok"})
		response, _ := json.Marshal(RPCResponse{RequestID: requestID, Data: responseData})
		handleSpokeMessage("rpc-node", mustEnvelope("response", response))
	}
	GlobalHub.Lock()
	GlobalHub.Spokes["rpc-node"] = mws
	GlobalHub.Unlock()
	t.Cleanup(func() {
		GlobalHub.Lock()
		delete(GlobalHub.Spokes, "rpc-node")
		GlobalHub.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := CallSpoke(ctx, "rpc-node", "ping", map[string]string{"value": "test"})
	assert.NoError(t, err)
	assert.JSONEq(t, `{"status":"ok"}`, string(result))
}

func mustEnvelope(messageType string, data []byte) []byte {
	payload, _ := json.Marshal(map[string]interface{}{"type": messageType, "data": json.RawMessage(data)})
	return payload
}

func TestSendExecInput(t *testing.T) {
	// Not connected
	err := SendExecInput("node_not_found", "exec1", []byte("ls"))
	assert.Error(t, err)

	// Connected
	mws := &mockWSConn{}
	GlobalHub.Lock()
	GlobalHub.Spokes["node_exec"] = mws
	GlobalHub.Unlock()

	err = SendExecInput("node_exec", "exec1", []byte("ls"))
	assert.NoError(t, err)
	assert.Len(t, mws.writes, 1)
}
