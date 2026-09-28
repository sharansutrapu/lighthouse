package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"lighthouse/cluster"

	"github.com/stretchr/testify/assert"
)

type resourceRPCRequest struct {
	Action string
	Data   interface{}
}

type resourceRPCConn struct {
	sync.Mutex
	requests  []resourceRPCRequest
	responses map[string]json.RawMessage
}

func (conn *resourceRPCConn) ReadMessage() (int, []byte, error) {
	return 0, nil, errors.New("not used")
}

func (conn *resourceRPCConn) WriteJSON(value interface{}) error {
	payload, ok := value.(map[string]interface{})
	if !ok {
		return errors.New("unexpected request payload")
	}
	requestID, _ := payload["request_id"].(string)
	action, _ := payload["action"].(string)
	conn.Lock()
	conn.requests = append(conn.requests, resourceRPCRequest{Action: action, Data: payload["data"]})
	response := conn.responses[action]
	conn.Unlock()

	cluster.GlobalHub.RLock()
	responseChannel := cluster.GlobalHub.PendingRequests[requestID]
	cluster.GlobalHub.RUnlock()
	if responseChannel == nil {
		return errors.New("request was not registered")
	}
	responseChannel <- cluster.RPCResponse{RequestID: requestID, Data: response}
	return nil
}

func (conn *resourceRPCConn) WriteMessage(int, []byte) error { return nil }
func (conn *resourceRPCConn) Close() error                   { return nil }

func configureResourceSpoke(t *testing.T, responses map[string]json.RawMessage) *resourceRPCConn {
	t.Helper()
	originalMode := LighthouseMode
	originalNodeID := NodeID
	LighthouseMode = "hub"
	NodeID = "hub-node"

	conn := &resourceRPCConn{responses: responses}
	cluster.GlobalHub.Lock()
	cluster.GlobalHub.Spokes["resource-spoke"] = conn
	cluster.GlobalHub.SpokeCapabilities["resource-spoke"] = map[string]bool{"resources": true}
	cluster.GlobalHub.Unlock()
	t.Cleanup(func() {
		LighthouseMode = originalMode
		NodeID = originalNodeID
		cluster.GlobalHub.Lock()
		delete(cluster.GlobalHub.Spokes, "resource-spoke")
		delete(cluster.GlobalHub.SpokeCapabilities, "resource-spoke")
		cluster.GlobalHub.Unlock()
	})
	return conn
}

func TestAggregateClusterResourcesTagsOwningNode(t *testing.T) {
	configureResourceSpoke(t, map[string]json.RawMessage{
		"list_images": json.RawMessage(`{"Items":[{"Id":"remote-image"}]}`),
	})

	items := aggregateClusterResources(context.Background(), "list_images", map[string]interface{}{
		"Items": []map[string]interface{}{{"Id": "local-image"}},
	})

	if assert.Len(t, items, 2) {
		assert.Equal(t, "local-image", items[0]["Id"])
		assert.Equal(t, "hub-node", items[0]["node_id"])
		assert.Equal(t, false, items[0]["is_remote"])
		assert.Equal(t, "remote-image", items[1]["Id"])
		assert.Equal(t, "resource-spoke", items[1]["node_id"])
		assert.Equal(t, true, items[1]["is_remote"])
	}
}

func TestResourceMutationsRouteToSelectedNode(t *testing.T) {
	conn := configureResourceSpoke(t, map[string]json.RawMessage{
		"image_remove":   json.RawMessage(`[]`),
		"volume_remove":  json.RawMessage(`{"message":"ok"}`),
		"network_remove": json.RawMessage(`{"message":"ok"}`),
		"image_prune":    json.RawMessage(`{"Report":{"SpaceReclaimed":0}}`),
		"volume_prune":   json.RawMessage(`{"Report":{"SpaceReclaimed":0}}`),
		"network_prune":  json.RawMessage(`{"Report":{}}`),
	})

	cli := mockDockerClientWithRoundTripper(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("node-targeted request unexpectedly reached the hub Docker daemon")
	})
	claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
	e, group, _, _ := setupEchoWithClaimsHelper(claims)
	RegisterImageRoutes(group, cli)
	RegisterVolumeRoutes(group, cli)
	RegisterNetworkRoutes(group, cli)

	tests := []struct {
		method string
		path   string
		body   string
		action string
	}{
		{http.MethodDelete, "/api/images/remote-image?node_id=resource-spoke", "", "image_remove"},
		{http.MethodDelete, "/api/volumes/remote-volume?node_id=resource-spoke", "", "volume_remove"},
		{http.MethodDelete, "/api/networks/remote-network?node_id=resource-spoke", "", "network_remove"},
		{http.MethodPost, "/api/images/prune?node_id=resource-spoke", `{}`, "image_prune"},
		{http.MethodPost, "/api/volumes/prune?node_id=resource-spoke", `{}`, "volume_prune"},
		{http.MethodPost, "/api/networks/prune?node_id=resource-spoke", `{}`, "network_prune"},
	}

	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}

	conn.Lock()
	defer conn.Unlock()
	if assert.Len(t, conn.requests, len(tests)) {
		for index, test := range tests {
			assert.Equal(t, test.action, conn.requests[index].Action)
		}
	}
}
