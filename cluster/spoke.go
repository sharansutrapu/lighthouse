package cluster

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moby/moby/client"

	"lighthouse/db"
	"lighthouse/scanner"
)

var spokeWs *websocket.Conn
var dockerClient *client.Client

// StartSpokeAgent connects to the Hub and handles communication
func StartSpokeAgent(hubURL, hubToken, nodeID string, cli *client.Client) {
	dockerClient = cli

	url := hubURL + "/api/spoke/connect?token=" + hubToken + "&node_id=" + nodeID
	
	for {
		log.Printf("[Spoke] Dialing Hub at %s", url)
		ws, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			log.Printf("[Spoke] Dial error: %v. Retrying in 5s...", err)
			time.Sleep(5 * time.Second)
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
				case <-time.After(5 * time.Second):
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
		time.Sleep(5 * time.Second)
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
	
	payload := map[string]interface{}{
		"type": msgType,
		"data": b,
	}
	
	err = spokeWs.WriteJSON(payload)
	if err != nil {
		log.Printf("[Spoke] Write error: %v", err)
	}
}

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

func handleCommand(action, containerID string) {
	ctx := context.Background()
	switch action {
	case "start":
		dockerClient.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
	case "stop":
		timeout := 10
		dockerClient.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout})
	case "restart":
		timeout := 10
		dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{Timeout: &timeout})
	case "delete":
		dockerClient.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
	case "scan":
		go func() {
			c, err := dockerClient.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
			if err != nil {
				log.Printf("[Spoke] scan error: container inspect failed: %v", err)
				return
			}
			imageName := c.Container.Config.Image
			log.Printf("[Spoke] Scanning image %s...", imageName)
			res, err := scanner.ScanImage(ctx, dockerClient, imageName)
			if err != nil {
				log.Printf("[Spoke] scan error: %v", err)
				return
			}
			b, _ := json.Marshal(res)
			db.GormDB.Create(&db.ImageScanResult{
				Image:  imageName,
				Result: string(b),
			})
			log.Printf("[Spoke] Scan complete for %s", imageName)
		}()
	}
}

func handleExecSession(execID, containerID string) {
	// Not fully implemented yet due to complexity of streaming Docker attach/exec API
	// But architecture handles it.
}
