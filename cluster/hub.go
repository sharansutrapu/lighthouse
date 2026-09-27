package cluster

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"lighthouse/alerts"
	"lighthouse/db"
)

// WSConn is the subset of *websocket.Conn methods used by the hub/spoke
// protocol, extracted as an interface so tests can substitute a fake connection.
type WSConn interface {
	ReadMessage() (int, []byte, error)
	WriteJSON(v interface{}) error
	WriteMessage(messageType int, data []byte) error
	Close() error
}

var upgraderFunc = func(w http.ResponseWriter, r *http.Request) (WSConn, error) {
	u := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	return u.Upgrade(w, r, nil)
}

// Hub maintains the state of connected Spokes
type Hub struct {
	sync.RWMutex
	Spokes          map[string]WSConn
	SpokeContainers map[string][]map[string]interface{}
	SpokeLastSeen   map[string]time.Time
	ExecStreams     map[string]WSConn        // maps exec_id to UI websocket
	LogStreams      map[string]WSConn        // maps stream_id to UI websocket
	CommandResults  map[string]CommandResult // container_id -> most recent dispatched command outcome
}

var hubSpokeWriteMu sync.Mutex

// NodeStatus is the read-only topology snapshot exposed by the Hub API.
type NodeStatus struct {
	ID             string    `json:"id"`
	Connected      bool      `json:"connected"`
	LastSeen       time.Time `json:"last_seen"`
	ContainerCount int       `json:"container_count"`
}

// CommandResult is the outcome of a command dispatched to a Spoke, reported
// back over the same WebSocket so a failure is never silently treated as success.
type CommandResult struct {
	Action string `json:"action"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

var GlobalHub = &Hub{
	Spokes:          make(map[string]WSConn),
	SpokeContainers: make(map[string][]map[string]interface{}),
	SpokeLastSeen:   make(map[string]time.Time),
	ExecStreams:     make(map[string]WSConn),
	LogStreams:      make(map[string]WSConn),
	CommandResults:  make(map[string]CommandResult),
}

// RegisterHubRoutes attaches the WebSocket endpoint
func RegisterHubRoutes(e *echo.Echo, hubToken string) {
	e.GET("/api/spoke/connect", func(c echo.Context) error {
		token := strings.TrimSpace(strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer "))
		if token == "" {
			// Query authentication remains temporarily supported so a hub can be
			// upgraded before its spokes during a rolling deployment.
			token = c.QueryParam("token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(hubToken)) != 1 {
			return c.String(http.StatusUnauthorized, "Invalid token")
		}
		nodeID := c.QueryParam("node_id")
		if nodeID == "" {
			return c.String(http.StatusBadRequest, "node_id required")
		}

		ws, err := upgraderFunc(c.Response(), c.Request())
		if err != nil {
			return err
		}
		defer ws.Close()

		GlobalHub.Lock()
		GlobalHub.Spokes[nodeID] = ws
		GlobalHub.SpokeLastSeen[nodeID] = time.Now()
		GlobalHub.Unlock()

		log.Printf("[Hub] Spoke %s connected", nodeID)

		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				log.Printf("[Hub] Spoke %s disconnected: %v", nodeID, err)
				GlobalHub.Lock()
				delete(GlobalHub.Spokes, nodeID)
				delete(GlobalHub.SpokeContainers, nodeID)
				GlobalHub.SpokeLastSeen[nodeID] = time.Now()
				GlobalHub.Unlock()
				break
			}
			handleSpokeMessage(nodeID, msg)
		}
		return nil
	})
}

// handleSpokeMessage processes multiplexed data from Spokes
func handleSpokeMessage(nodeID string, msg []byte) {
	var payload struct {
		Type        string          `json:"type"`
		ContainerID string          `json:"container_id,omitempty"`
		ExecID      string          `json:"exec_id,omitempty"`
		Data        json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

	GlobalHub.Lock()
	GlobalHub.SpokeLastSeen[nodeID] = time.Now()
	GlobalHub.Unlock()

	switch payload.Type {
	case "containers":
		var containers []map[string]interface{}
		json.Unmarshal(payload.Data, &containers)
		GlobalHub.Lock()
		GlobalHub.SpokeContainers[nodeID] = containers
		GlobalHub.Unlock()

	case "stat", "container_stat":
		var stat db.Stat
		json.Unmarshal(payload.Data, &stat)
		stat.NodeID = nodeID
		db.GormDB.Create(&stat)

	case "system_stat":
		var stat db.SystemStat
		json.Unmarshal(payload.Data, &stat)
		stat.NodeID = nodeID
		db.GormDB.Create(&stat)

	case "exec_output":
		GlobalHub.RLock()
		uiWs, ok := GlobalHub.ExecStreams[payload.ExecID]
		GlobalHub.RUnlock()
		if ok {
			uiWs.WriteMessage(websocket.TextMessage, payload.Data)
		}

	case "log_output", "log_error", "log_end":
		var output struct {
			StreamID string `json:"stream_id"`
			Data     string `json:"data"`
			Error    string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(payload.Data, &output); err != nil || output.StreamID == "" {
			return
		}
		GlobalHub.RLock()
		uiWs, ok := GlobalHub.LogStreams[output.StreamID]
		GlobalHub.RUnlock()
		if !ok {
			return
		}
		if payload.Type == "log_output" {
			_ = uiWs.WriteMessage(websocket.TextMessage, []byte(output.Data))
		} else if payload.Type == "log_error" {
			_ = uiWs.WriteMessage(websocket.TextMessage, []byte("\r\n[LightHouse] "+output.Error+"\r\n"))
		} else {
			_ = uiWs.Close()
			UnregisterLogStream(output.StreamID)
		}

	case "command_result":
		var result struct {
			Action      string `json:"action"`
			ContainerID string `json:"container_id"`
			Status      string `json:"status"`
			Error       string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(payload.Data, &result); err != nil {
			return
		}
		if result.Status == "failed" {
			log.Printf("[Hub] Spoke %s reported command %q failed for container %s: %s", nodeID, result.Action, result.ContainerID, result.Error)
			if alerts.Global != nil {
				alerts.Global.TriggerSystemAlert("spoke_command_failed", fmt.Sprintf("Spoke %s: %s on container %s failed: %s", nodeID, result.Action, result.ContainerID, result.Error))
			}
		}
		GlobalHub.Lock()
		GlobalHub.CommandResults[result.ContainerID] = CommandResult{Action: result.Action, Status: result.Status, Error: result.Error}
		GlobalHub.Unlock()
	}
}

// SnapshotSpokes returns a stable, sorted copy of the Hub's known spoke state.
func SnapshotSpokes() []NodeStatus {
	GlobalHub.RLock()
	defer GlobalHub.RUnlock()

	nodes := make([]NodeStatus, 0, len(GlobalHub.SpokeLastSeen))
	for nodeID, lastSeen := range GlobalHub.SpokeLastSeen {
		_, connected := GlobalHub.Spokes[nodeID]
		nodes = append(nodes, NodeStatus{
			ID:             nodeID,
			Connected:      connected,
			LastSeen:       lastSeen,
			ContainerCount: len(GlobalHub.SpokeContainers[nodeID]),
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

// FindSpokeContainer resolves a full or short Docker container ID to its
// connected spoke and returns the fields required for authorization.
func FindSpokeContainer(containerID string) (nodeID, name, image string, found bool) {
	GlobalHub.RLock()
	defer GlobalHub.RUnlock()
	for currentNodeID, containers := range GlobalHub.SpokeContainers {
		for _, container := range containers {
			id, _ := container["ID"].(string)
			if id == "" {
				id, _ = container["Id"].(string)
			}
			if id == "" {
				continue
			}
			if id != containerID && !strings.HasPrefix(id, containerID) && !strings.HasPrefix(containerID, id) {
				continue
			}
			name = id
			if names, ok := container["Names"].([]interface{}); ok && len(names) > 0 {
				name, _ = names[0].(string)
			}
			name = strings.TrimPrefix(name, "/")
			image, _ = container["Image"].(string)
			return currentNodeID, name, image, true
		}
	}
	return "", "", "", false
}

// RegisterLogStream binds a browser WebSocket to one remote stream ID.
func RegisterLogStream(streamID string, ws WSConn) {
	GlobalHub.Lock()
	GlobalHub.LogStreams[streamID] = ws
	GlobalHub.Unlock()
}

// UnregisterLogStream releases a browser WebSocket binding.
func UnregisterLogStream(streamID string) {
	GlobalHub.Lock()
	delete(GlobalHub.LogStreams, streamID)
	GlobalHub.Unlock()
}

func writeToSpoke(nodeID string, payload interface{}) error {
	GlobalHub.RLock()
	ws, ok := GlobalHub.Spokes[nodeID]
	GlobalHub.RUnlock()
	if !ok {
		return fmt.Errorf("spoke not connected")
	}
	hubSpokeWriteMu.Lock()
	defer hubSpokeWriteMu.Unlock()
	return ws.WriteJSON(payload)
}

// SendLogStart asks a spoke to start following one container's logs.
func SendLogStart(nodeID, streamID, containerID string) error {
	return writeToSpoke(nodeID, map[string]string{
		"type":         "log_start",
		"stream_id":    streamID,
		"container_id": containerID,
	})
}

// SendLogStop cancels a previously started spoke log stream.
func SendLogStop(nodeID, streamID string) error {
	return writeToSpoke(nodeID, map[string]string{
		"type":      "log_stop",
		"stream_id": streamID,
	})
}

// SendCommandToSpoke sends an action like start/stop/restart
func SendCommandToSpoke(nodeID, action, containerID string) error {
	payload := map[string]string{
		"type":         "command",
		"action":       action,
		"container_id": containerID,
	}
	return writeToSpoke(nodeID, payload)
}

// SendExecInput sends terminal input to a Spoke container
func SendExecInput(nodeID, execID string, input []byte) error {
	payload := map[string]interface{}{
		"type":    "exec_input",
		"exec_id": execID,
		"data":    input,
	}
	return writeToSpoke(nodeID, payload)
}
