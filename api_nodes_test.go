package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lighthouse/cluster"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestHandleGETNodes(t *testing.T) {
	originalMode := LighthouseMode
	originalNodeID := NodeID
	LighthouseMode = "hub"
	NodeID = "hub-1"

	cluster.GlobalHub.Lock()
	originalSpokes := cluster.GlobalHub.Spokes
	originalContainers := cluster.GlobalHub.SpokeContainers
	originalLastSeen := cluster.GlobalHub.SpokeLastSeen
	originalCapabilities := cluster.GlobalHub.SpokeCapabilities
	cluster.GlobalHub.Spokes = map[string]cluster.WSConn{"spoke-1": &mockNodeWSConn{}}
	cluster.GlobalHub.SpokeContainers = map[string][]map[string]interface{}{"spoke-1": {{"ID": "c1"}}}
	cluster.GlobalHub.SpokeLastSeen = map[string]time.Time{"spoke-1": time.Unix(100, 0)}
	cluster.GlobalHub.SpokeCapabilities = map[string]map[string]bool{"spoke-1": {"logs": true, "shell": true}}
	cluster.GlobalHub.Unlock()

	defer func() {
		LighthouseMode = originalMode
		NodeID = originalNodeID
		cluster.GlobalHub.Lock()
		cluster.GlobalHub.Spokes = originalSpokes
		cluster.GlobalHub.SpokeContainers = originalContainers
		cluster.GlobalHub.SpokeLastSeen = originalLastSeen
		cluster.GlobalHub.SpokeCapabilities = originalCapabilities
		cluster.GlobalHub.Unlock()
	}()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/nodes", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	assert.NoError(t, handleGETNodes()(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var nodes []nodeResponse
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &nodes))
	if assert.Len(t, nodes, 2) {
		assert.Equal(t, "hub-1", nodes[0].ID)
		assert.Equal(t, "spoke-1", nodes[1].ID)
		assert.True(t, nodes[1].Connected)
		assert.Equal(t, 1, nodes[1].ContainerCount)
		assert.Equal(t, []string{"logs", "shell"}, nodes[1].Capabilities)
	}
}

type mockNodeWSConn struct{}

func (m *mockNodeWSConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (m *mockNodeWSConn) WriteJSON(interface{}) error       { return nil }
func (m *mockNodeWSConn) WriteMessage(int, []byte) error    { return nil }
func (m *mockNodeWSConn) Close() error                      { return nil }
