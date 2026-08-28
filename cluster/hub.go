package cluster

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

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
	ExecStreams     map[string]WSConn // maps exec_id to UI websocket
	CommandResults  map[string]CommandResult // container_id -> most recent dispatched command outcome
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
	ExecStreams:     make(map[string]WSConn),
	CommandResults:  make(map[string]CommandResult),
}

// RegisterHubRoutes attaches the WebSocket endpoint
func RegisterHubRoutes(e *echo.Echo, hubToken string) {
	e.GET("/api/spoke/connect", func(c echo.Context) error {
		token := c.QueryParam("token")
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
		GlobalHub.Unlock()

		log.Printf("[Hub] Spoke %s connected", nodeID)

		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				log.Printf("[Hub] Spoke %s disconnected: %v", nodeID, err)
				GlobalHub.Lock()
				delete(GlobalHub.Spokes, nodeID)
				delete(GlobalHub.SpokeContainers, nodeID)
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

	switch payload.Type {
	case "containers":
		var containers []map[string]interface{}
		json.Unmarshal(payload.Data, &containers)
		GlobalHub.Lock()
		GlobalHub.SpokeContainers[nodeID] = containers
		GlobalHub.Unlock()

	case "stat":
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

// SendCommandToSpoke sends an action like start/stop/restart
func SendCommandToSpoke(nodeID, action, containerID string) error {
	GlobalHub.RLock()
	ws, ok := GlobalHub.Spokes[nodeID]
	GlobalHub.RUnlock()

	if !ok {
		return fmt.Errorf("spoke not connected")
	}

	payload := map[string]string{
		"type":         "command",
		"action":       action,
		"container_id": containerID,
	}
	return ws.WriteJSON(payload)
}

// SendExecInput sends terminal input to a Spoke container
func SendExecInput(nodeID, execID string, input []byte) error {
	GlobalHub.RLock()
	ws, ok := GlobalHub.Spokes[nodeID]
	GlobalHub.RUnlock()

	if !ok {
		return fmt.Errorf("spoke not connected")
	}

	payload := map[string]interface{}{
		"type":    "exec_input",
		"exec_id": execID,
		"data":    input,
	}
	return ws.WriteJSON(payload)
}
