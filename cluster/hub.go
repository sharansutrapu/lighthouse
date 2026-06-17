package cluster

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"lighthouse/db"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub maintains the state of connected Spokes
type Hub struct {
	sync.RWMutex
	Spokes          map[string]*websocket.Conn
	SpokeContainers map[string][]map[string]interface{}
	ExecStreams     map[string]*websocket.Conn // maps exec_id to UI websocket
}

var GlobalHub = &Hub{
	Spokes:          make(map[string]*websocket.Conn),
	SpokeContainers: make(map[string][]map[string]interface{}),
	ExecStreams:     make(map[string]*websocket.Conn),
}

// RegisterHubRoutes attaches the WebSocket endpoint
func RegisterHubRoutes(e *echo.Echo, hubToken string) {
	e.GET("/api/spoke/connect", func(c echo.Context) error {
		token := c.QueryParam("token")
		if token != hubToken {
			return c.String(http.StatusUnauthorized, "Invalid token")
		}
		nodeID := c.QueryParam("node_id")
		if nodeID == "" {
			return c.String(http.StatusBadRequest, "node_id required")
		}

		ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
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
