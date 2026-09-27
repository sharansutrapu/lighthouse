package cluster

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"lighthouse/db"
	"lighthouse/scanner"
)

var spokeWs WSConn
var dialFunc = func(url string, requestHeader http.Header) (WSConn, error) {
	ws, _, err := websocket.DefaultDialer.Dial(url, requestHeader)
	return ws, err
}
var dockerClient *client.Client
var spokeWriteMu sync.Mutex

var activeLogStreams = struct {
	sync.Mutex
	cancels map[string]context.CancelFunc
}{cancels: make(map[string]context.CancelFunc)}

var syncInterval = 5 * time.Second
var reconnectInterval = 5 * time.Second

var agentRunning = true

// StartSpokeAgent connects to the Hub and handles communication
func StartSpokeAgent(hubURL, hubToken, nodeID string, cli *client.Client) {
	dockerClient = cli

	connectURL := hubURL + "/api/spoke/connect?node_id=" + url.QueryEscape(nodeID)
	requestHeader := http.Header{}
	requestHeader.Set("Authorization", "Bearer "+hubToken)

	for agentRunning {
		log.Printf("[Spoke] Connecting to Hub as node %s", nodeID)
		ws, err := dialFunc(connectURL, requestHeader)
		if err != nil {
			log.Printf("[Spoke] Dial error: %v. Retrying in 5s...", err)
			time.Sleep(reconnectInterval)
			continue
		}

		spokeWriteMu.Lock()
		spokeWs = ws
		spokeWriteMu.Unlock()
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
		cancelAllLogStreams()
		spokeWriteMu.Lock()
		if spokeWs == ws {
			spokeWs = nil
		}
		spokeWriteMu.Unlock()
		ws.Close()
		time.Sleep(reconnectInterval)
	}
}

// PushToHub allows other packages (like collectStats) to push JSON messages
func PushToHub(msgType string, data interface{}) {
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

	spokeWriteMu.Lock()
	defer spokeWriteMu.Unlock()
	if spokeWs == nil {
		return
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
		StreamID    string `json:"stream_id,omitempty"`
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
	} else if payload.Type == "log_start" {
		go handleLogStream(payload.StreamID, payload.ContainerID)
	} else if payload.Type == "log_stop" {
		stopLogStream(payload.StreamID)
	}
}

type logStreamMessage struct {
	StreamID string `json:"stream_id"`
	Data     string `json:"data,omitempty"`
	Error    string `json:"error,omitempty"`
}

type hubLogWriter struct {
	streamID string
}

func (w *hubLogWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		PushToHub("log_output", logStreamMessage{StreamID: w.streamID, Data: string(p)})
	}
	return len(p), nil
}

func handleLogStream(streamID, containerID string) {
	if streamID == "" || containerID == "" || dockerClient == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	activeLogStreams.Lock()
	if previous := activeLogStreams.cancels[streamID]; previous != nil {
		previous()
	}
	activeLogStreams.cancels[streamID] = cancel
	activeLogStreams.Unlock()
	defer func() {
		cancel()
		activeLogStreams.Lock()
		delete(activeLogStreams.cancels, streamID)
		activeLogStreams.Unlock()
		PushToHub("log_end", logStreamMessage{StreamID: streamID})
	}()

	inspect, err := dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		PushToHub("log_error", logStreamMessage{StreamID: streamID, Error: "Container not found on spoke"})
		return
	}

	out, err := dockerClient.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "100",
		Timestamps: true,
	})
	if err != nil {
		PushToHub("log_error", logStreamMessage{StreamID: streamID, Error: "Failed to fetch container logs"})
		return
	}
	defer out.Close()

	writer := &hubLogWriter{streamID: streamID}
	if inspect.Container.Config != nil && inspect.Container.Config.Tty {
		_, err = io.Copy(writer, out)
	} else {
		_, err = stdcopy.StdCopy(writer, writer, out)
	}
	if err != nil && ctx.Err() == nil {
		PushToHub("log_error", logStreamMessage{StreamID: streamID, Error: "Remote log stream ended unexpectedly"})
	}
}

func stopLogStream(streamID string) {
	activeLogStreams.Lock()
	cancel := activeLogStreams.cancels[streamID]
	delete(activeLogStreams.cancels, streamID)
	activeLogStreams.Unlock()
	if cancel != nil {
		cancel()
	}
}

func cancelAllLogStreams() {
	activeLogStreams.Lock()
	cancels := make([]context.CancelFunc, 0, len(activeLogStreams.cancels))
	for streamID, cancel := range activeLogStreams.cancels {
		cancels = append(cancels, cancel)
		delete(activeLogStreams.cancels, streamID)
	}
	activeLogStreams.Unlock()
	for _, cancel := range cancels {
		cancel()
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
