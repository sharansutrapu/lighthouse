package cluster

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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

	// Call default upgraderFunc to cover it
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	_, _ = upgraderFunc(rec, req) // It will return err since not a real ws req, but covers the code

	RegisterHubRoutes(e, "secret")

	// Invalid token
	req = httptest.NewRequest(http.MethodGet, "/api/spoke/connect?token=wrong", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Missing node_id
	req = httptest.NewRequest(http.MethodGet, "/api/spoke/connect?token=secret", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Upgrader error
	upgraderFunc = func(w http.ResponseWriter, r *http.Request) (WSConn, error) {
		return nil, errors.New("upgrade error")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/spoke/connect?token=secret&node_id=node1", nil)
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
	req = httptest.NewRequest(http.MethodGet, "/api/spoke/connect?token=secret&node_id=node2", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	GlobalHub.RLock()
	_, exists := GlobalHub.Spokes["node2"]
	GlobalHub.RUnlock()
	assert.False(t, exists, "Spoke should be removed after disconnect")
}

func TestHandleSpokeMessage(t *testing.T) {
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
	// (Check DB manually or ignore)

	// system_stat
	msg = []byte(`{"type":"system_stat","data":{"cpu": 20.5}}`)
	handleSpokeMessage("node1", msg)

	// exec_output
	uiWs := &mockWSConn{}
	GlobalHub.Lock()
	GlobalHub.ExecStreams["exec1"] = uiWs
	GlobalHub.Unlock()
	msg = []byte(`{"type":"exec_output","exec_id":"exec1","data":"hello"}`)
	handleSpokeMessage("node1", msg)
	assert.Len(t, uiWs.writes, 1)

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
