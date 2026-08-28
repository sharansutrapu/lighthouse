package cluster

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moby/moby/client"

	"lighthouse/db"
	"lighthouse/scanner"
)

var spokeWs WSConn
var dialFunc = func(url string) (WSConn, error) {
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	return ws, err
}
var dockerClient *client.Client

var syncInterval = 5 * time.Second
var reconnectInterval = 5 * time.Second

var agentRunning = true

// StartSpokeAgent connects to the Hub and handles communication
func StartSpokeAgent(hubURL, hubToken, nodeID string, cli *client.Client) {
	dockerClient = cli

	connectURL := hubURL + "/api/spoke/connect?token=" + url.QueryEscape(hubToken) + "&node_id=" + url.QueryEscape(nodeID)

	for agentRunning {
		log.Printf("[Spoke] Dialing Hub at %s", connectURL)
		ws, err := dialFunc(connectURL)
		if err != nil {
			log.Printf("[Spoke] Dial error: %v. Retrying in 5s...", err)
			time.Sleep(reconnectInterval)
			continue
		}

		spokeWs = ws
		log.Printf("[Spoke] Connected to Hub successfully")

		// Start background syncer for container list
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(syncInterval):
					res, err := dockerClient.ContainerList(context.Background(), client.ContainerListOptions{All: true})
					if err == nil {
						PushToHub("containers", res.Items)
					}
				}
			}
		}()

		// Listen for commands
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				log.Printf("[Spoke] Hub connection lost: %v", err)
				break
			}
			handleHubMessage(msg)
		}

		cancel()
		ws.Close()
		time.Sleep(reconnectInterval)
	}
}

// PushToHub allows other packages (like collectStats) to push JSON messages
func PushToHub(msgType string, data interface{}) {
	if spokeWs == nil {
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}

	// json.RawMessage (not a bare []byte) is required here: encoding/json
	// base64-encodes plain []byte values, which would silently turn the
	// already-JSON-encoded `b` into an opaque base64 string on the wire —
	// handleSpokeMessage's json.Unmarshal(payload.Data, &X) would then fail
	// (silently, since its error return isn't checked) for every message
	// type, every time. json.RawMessage implements json.Marshaler so it is
	// embedded verbatim instead.
	payload := map[string]interface{}{
		"type": msgType,
		"data": json.RawMessage(b),
	}

	err = spokeWs.WriteJSON(payload)
	if err != nil {
		log.Printf("[Spoke] Write error: %v", err)
	}
}

// handleHubMessage dispatches one multiplexed JSON message received from the
// Hub over the spoke's WebSocket connection to the appropriate handler based
// on its "type" field.
func handleHubMessage(msg []byte) {
	var payload struct {
		Type        string `json:"type"`
		Action      string `json:"action,omitempty"`
		ContainerID string `json:"container_id,omitempty"`
		ExecID      string `json:"exec_id,omitempty"`
		Data        []byte `json:"data,omitempty"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

	if payload.Type == "command" {
		handleCommand(payload.Action, payload.ContainerID)
	} else if payload.Type == "exec_start" {
		// Start a terminal session and stream output
		// Note: Simplified for demonstration; proper terminal multiplexing requires full attach/exec flow.
		go handleExecSession(payload.ExecID, payload.ContainerID)
	} else if payload.Type == "exec_input" {
		// TODO: write to exec stdin
	}
}

// scanImageFunc is overridable in tests; in production it calls the real
// scanner package to run Trivy against an image.
var scanImageFunc = scanner.ScanImageFunc

// handleCommand executes one Hub-dispatched container action (start, stop,
// restart, delete, or scan) against this Spoke's local Docker daemon, and
// always reports the outcome back to the Hub via PushToHub so a failure is
// never silently swallowed.
func handleCommand(action, containerID string) {
	ctx := context.Background()
	var err error
	switch action {
	case "start":
		_, err = dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
	case "stop":
		timeout := 10
		_, err = dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout})
	case "restart":
		timeout := 10
		_, err = dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{Timeout: &timeout})
	case "delete":
		_, err = dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
	case "scan":
		go func() {
			c, inspectErr := dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
			if inspectErr != nil {
				log.Printf("[Spoke] scan error: container inspect failed: %v", inspectErr)
				PushToHub("command_result", commandResult{Action: "scan", ContainerID: containerID, Status: "failed", Error: inspectErr.Error()})
				return
			}
			imageName := c.Container.Config.Image
			log.Printf("[Spoke] Scanning image %s...", imageName)
			res, scanErr := scanImageFunc(ctx, dockerClient, imageName)
			if scanErr != nil {
				log.Printf("[Spoke] scan error: %v", scanErr)
				PushToHub("command_result", commandResult{Action: "scan", ContainerID: containerID, Status: "failed", Error: scanErr.Error()})
				return
			}
			b, _ := json.Marshal(res)
			db.GormDB.Create(&db.ImageScanResult{
				Image:  imageName,
				Result: string(b),
			})
			log.Printf("[Spoke] Scan complete for %s", imageName)
			PushToHub("command_result", commandResult{Action: "scan", ContainerID: containerID, Status: "success"})
		}()
		return
	default:
		log.Printf("[Spoke] unknown command action %q", action)
		return
	}

	if err != nil {
		log.Printf("[Spoke] command %q on container %s failed: %v", action, containerID, err)
		PushToHub("command_result", commandResult{Action: action, ContainerID: containerID, Status: "failed", Error: err.Error()})
		return
	}
	PushToHub("command_result", commandResult{Action: action, ContainerID: containerID, Status: "success"})
}

// commandResult reports the outcome of a Hub-dispatched command back to the
// Hub — without this, a failed start/stop/restart/delete on a Spoke is
// silently dropped and the Hub (and the UI) assumes it succeeded.
type commandResult struct {
	Action      string `json:"action"`
	ContainerID string `json:"container_id"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// handleExecSession would stream an interactive shell session for a
// Hub-initiated exec request. Not yet implemented — shell access currently
// only works against containers on the node the UI talks to directly.
func handleExecSession(execID, containerID string) {
	log.Printf("[Spoke] Exec session %s for container %s", execID, containerID)
}
