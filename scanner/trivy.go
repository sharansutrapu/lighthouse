package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/moby/moby/client"
)

var scanSem = make(chan struct{}, 2) // Limit to 2 concurrent trivy scans

// ScanImage runs aquasec/trivy against the given docker image name using the local docker binary.
// This requires the host to have docker installed and accessible.
func ScanImage(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
	log.Printf("Queuing trivy scan for image: %s", imageName)

	// Acquire semaphore to limit concurrency
	scanSem <- struct{}{}
	defer func() { <-scanSem }()

	log.Printf("Starting trivy scan for image: %s", imageName)

	// Apply a 5-minute timeout specifically for the active execution phase, independent of queue wait time
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "docker", "run", "--rm", 
		"-v", "/var/run/docker.sock:/var/run/docker.sock", 
		"aquasec/trivy:latest", "image", "-f", "json", "--quiet", "--timeout", "5m", imageName)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("trivy scan failed: %v, stderr: %s", err, stderr.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("failed to parse trivy json output: %v", err)
	}

	return result, nil
}
