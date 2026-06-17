package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"log"

	"github.com/moby/moby/client"
)

// ScanImage runs aquasec/trivy against the given docker image name using the local docker binary.
// This requires the host to have docker installed and accessible.
func ScanImage(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
	log.Printf("Starting trivy scan for image: %s", imageName)

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", 
		"-v", "/var/run/docker.sock:/var/run/docker.sock", 
		"aquasec/trivy:latest", "image", "-f", "json", "--quiet", imageName)

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
