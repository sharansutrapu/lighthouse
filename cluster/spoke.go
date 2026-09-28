package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
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

type execConnection interface {
	Write([]byte) (int, error)
	Close() error
}

type activeExecSession struct {
	conn   execConnection
	cancel context.CancelFunc
}

var activeExecSessions = struct {
	sync.Mutex
	sessions map[string]activeExecSession
	pending  map[string][][]byte
}{
	sessions: make(map[string]activeExecSession),
	pending:  make(map[string][][]byte),
}

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
		PushToHub("capabilities", map[string]interface{}{
			"protocol_version": 1,
			"capabilities": []string{
				"list", "metrics", "logs", "inspect", "actions", "scan", "shell", "resources",
			},
		})

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
		cancelAllExecSessions()
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
		Type        string          `json:"type"`
		Action      string          `json:"action,omitempty"`
		ContainerID string          `json:"container_id,omitempty"`
		ExecID      string          `json:"exec_id,omitempty"`
		StreamID    string          `json:"stream_id,omitempty"`
		RequestID   string          `json:"request_id,omitempty"`
		Shell       string          `json:"shell,omitempty"`
		Data        json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

	if payload.Type == "command" {
		handleCommand(payload.Action, payload.ContainerID)
	} else if payload.Type == "exec_start" {
		go handleExecSession(payload.ExecID, payload.ContainerID, payload.Shell)
	} else if payload.Type == "exec_input" {
		var input []byte
		if err := json.Unmarshal(payload.Data, &input); err == nil {
			writeExecInput(payload.ExecID, input)
		}
	} else if payload.Type == "exec_stop" {
		stopExecSession(payload.ExecID)
	} else if payload.Type == "log_start" {
		go handleLogStream(payload.StreamID, payload.ContainerID)
	} else if payload.Type == "log_stop" {
		stopLogStream(payload.StreamID)
	} else if payload.Type == "request" {
		go handleRPCRequest(payload.RequestID, payload.Action, payload.Data)
	}
}

type rpcResponse struct {
	RequestID string          `json:"request_id"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     string          `json:"error,omitempty"`
}

func respondToHub(requestID string, data interface{}, responseErr error) {
	response := rpcResponse{RequestID: requestID}
	if responseErr != nil {
		response.Error = responseErr.Error()
	} else if data != nil {
		response.Data, _ = json.Marshal(data)
	}
	PushToHub("response", response)
}

func handleRPCRequest(requestID, action string, data json.RawMessage) {
	if requestID == "" || dockerClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	switch action {
	case "container_inspect":
		var request struct {
			ContainerID string `json:"container_id"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			respondToHub(requestID, nil, err)
			return
		}
		result, err := dockerClient.ContainerInspect(ctx, request.ContainerID, client.ContainerInspectOptions{Size: true})
		respondToHub(requestID, result, err)
	case "container_action":
		var request struct {
			ContainerID string `json:"container_id"`
			Action      string `json:"action"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			respondToHub(requestID, nil, err)
			return
		}
		respondToHub(requestID, map[string]string{"status": "success"}, executeContainerAction(ctx, request.Action, request.ContainerID))
	case "list_images":
		result, err := dockerClient.ImageList(ctx, client.ImageListOptions{All: false})
		respondToHub(requestID, result, err)
	case "list_volumes":
		result, err := dockerClient.VolumeList(ctx, client.VolumeListOptions{})
		respondToHub(requestID, result, err)
	case "list_networks":
		result, err := dockerClient.NetworkList(ctx, client.NetworkListOptions{})
		respondToHub(requestID, result, err)
	case "image_remove":
		var request struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			respondToHub(requestID, nil, err)
			return
		}
		result, err := dockerClient.ImageRemove(ctx, request.ID, client.ImageRemoveOptions{Force: true, PruneChildren: true})
		respondToHub(requestID, result, err)
	case "volume_remove":
		var request struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			respondToHub(requestID, nil, err)
			return
		}
		_, err := dockerClient.VolumeRemove(ctx, request.Name, client.VolumeRemoveOptions{Force: true})
		respondToHub(requestID, map[string]string{"message": "Volume deleted successfully"}, err)
	case "network_remove":
		var request struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			respondToHub(requestID, nil, err)
			return
		}
		_, err := dockerClient.NetworkRemove(ctx, request.ID, client.NetworkRemoveOptions{})
		respondToHub(requestID, map[string]string{"message": "Network deleted successfully"}, err)
	case "image_prune":
		var request struct {
			AllUnused        bool `json:"all_unused"`
			RemoveContainers bool `json:"remove_containers"`
		}
		_ = json.Unmarshal(data, &request)
		warning := pruneContainersIfRequested(ctx, request.RemoveContainers)
		filters := make(client.Filters)
		if request.AllUnused {
			filters.Add("dangling", "false")
		} else {
			filters.Add("dangling", "true")
		}
		result, err := dockerClient.ImagePrune(ctx, client.ImagePruneOptions{Filters: filters})
		respondToHub(requestID, map[string]interface{}{"Report": result, "Warning": warning}, err)
	case "volume_prune":
		var request struct {
			RemoveContainers bool `json:"remove_containers"`
		}
		_ = json.Unmarshal(data, &request)
		warning := pruneContainersIfRequested(ctx, request.RemoveContainers)
		result, err := dockerClient.VolumePrune(ctx, client.VolumePruneOptions{})
		respondToHub(requestID, map[string]interface{}{"Report": result, "Warning": warning}, err)
	case "network_prune":
		var request struct {
			RemoveContainers bool `json:"remove_containers"`
		}
		_ = json.Unmarshal(data, &request)
		warning := pruneContainersIfRequested(ctx, request.RemoveContainers)
		result, err := dockerClient.NetworkPrune(ctx, client.NetworkPruneOptions{})
		respondToHub(requestID, map[string]interface{}{"Report": result, "Warning": warning}, err)
	default:
		respondToHub(requestID, nil, fmt.Errorf("unsupported remote action: %s", action))
	}
}

func pruneContainersIfRequested(ctx context.Context, removeContainers bool) string {
	if removeContainers {
		_, _ = dockerClient.ContainerPrune(ctx, client.ContainerPruneOptions{})
		return ""
	}
	filters := make(client.Filters)
	filters.Add("status", "exited", "created")
	stopped, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err == nil && len(stopped.Items) > 0 {
		return "Stopped containers detected. Some resources may not have been pruned."
	}
	return ""
}

func executeContainerAction(ctx context.Context, action, containerID string) error {
	switch action {
	case "start":
		_, err := dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
		return err
	case "stop":
		timeout := 60
		_, err := dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout})
		return err
	case "restart":
		timeout := 60
		_, err := dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{Timeout: &timeout})
		return err
	case "remove":
		_, err := dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
		return err
	default:
		return fmt.Errorf("unsupported container action: %s", action)
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
			containerName := strings.TrimPrefix(c.Container.Name, "/")
			PushToHub("scan_result", map[string]interface{}{
				"container_id":   containerID,
				"container_name": containerName,
				"image":          imageName,
				"result":         json.RawMessage(b),
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

type execStreamMessage struct {
	ExecID string `json:"exec_id"`
	Data   string `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}

func handleExecSession(execID, containerID, shell string) {
	if execID == "" {
		return
	}
	if containerID == "" || dockerClient == nil {
		failExecSession(execID, "Remote terminal is unavailable")
		return
	}
	allowedShells := map[string]bool{"/bin/sh": true, "/bin/bash": true, "/bin/ash": true}
	if !allowedShells[shell] {
		failExecSession(execID, "Invalid shell")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	execResult, err := dockerClient.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          true,
		Cmd:          []string{shell},
	})
	if err != nil {
		cancel()
		failExecSession(execID, "Failed to create terminal session")
		return
	}
	attached, err := dockerClient.ExecAttach(ctx, execResult.ID, client.ExecAttachOptions{TTY: true})
	if err != nil {
		cancel()
		failExecSession(execID, "Failed to attach terminal session")
		return
	}

	activeExecSessions.Lock()
	if previous, ok := activeExecSessions.sessions[execID]; ok {
		previous.cancel()
		_ = previous.conn.Close()
	}
	activeExecSessions.sessions[execID] = activeExecSession{conn: attached.Conn, cancel: cancel}
	for _, input := range activeExecSessions.pending[execID] {
		_, _ = attached.Conn.Write(input)
	}
	delete(activeExecSessions.pending, execID)
	activeExecSessions.Unlock()

	defer func() {
		cancel()
		attached.Close()
		activeExecSessions.Lock()
		delete(activeExecSessions.sessions, execID)
		activeExecSessions.Unlock()
		PushToHub("exec_end", execStreamMessage{ExecID: execID})
	}()

	buffer := make([]byte, 4096)
	for {
		count, readErr := attached.Reader.Read(buffer)
		if count > 0 {
			PushToHub("exec_output", execStreamMessage{ExecID: execID, Data: string(buffer[:count])})
		}
		if readErr != nil {
			if readErr != io.EOF && ctx.Err() == nil {
				PushToHub("exec_error", execStreamMessage{ExecID: execID, Error: "Remote terminal session ended unexpectedly"})
			}
			return
		}
	}
}

func failExecSession(execID, message string) {
	activeExecSessions.Lock()
	delete(activeExecSessions.pending, execID)
	activeExecSessions.Unlock()
	PushToHub("exec_error", execStreamMessage{ExecID: execID, Error: message})
	PushToHub("exec_end", execStreamMessage{ExecID: execID})
}

func writeExecInput(execID string, input []byte) {
	activeExecSessions.Lock()
	session, ok := activeExecSessions.sessions[execID]
	if ok {
		_, _ = session.conn.Write(input)
	} else {
		activeExecSessions.pending[execID] = append(activeExecSessions.pending[execID], append([]byte(nil), input...))
	}
	activeExecSessions.Unlock()
}

func stopExecSession(execID string) {
	activeExecSessions.Lock()
	session, ok := activeExecSessions.sessions[execID]
	delete(activeExecSessions.sessions, execID)
	delete(activeExecSessions.pending, execID)
	activeExecSessions.Unlock()
	if ok {
		session.cancel()
		_ = session.conn.Close()
	}
}

func cancelAllExecSessions() {
	activeExecSessions.Lock()
	sessions := make([]activeExecSession, 0, len(activeExecSessions.sessions))
	for execID, session := range activeExecSessions.sessions {
		sessions = append(sessions, session)
		delete(activeExecSessions.sessions, execID)
	}
	clear(activeExecSessions.pending)
	activeExecSessions.Unlock()
	for _, session := range sessions {
		session.cancel()
		_ = session.conn.Close()
	}
}
