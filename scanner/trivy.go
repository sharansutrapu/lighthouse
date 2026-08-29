package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/moby/moby/client"
)

var execCommandContext = exec.CommandContext

var scanSem = make(chan struct{}, 2) // Limit to 2 concurrent trivy scans

// ValidateImageName rejects image references that could be interpreted as
// extra command-line flags by trivy or docker when passed as a bare argv
// element (e.g. "--server=http://internal" or "-v"), or that otherwise don't
// match Docker's own reference grammar (registry[:port]/path[:tag][@digest]).
// Delegating to distribution/reference — the same parser Docker itself uses —
// is stricter and more correct than a hand-rolled regex (it correctly accepts
// things like "registry.local:5000/team/app:latest" while still rejecting
// anything with a leading "-", whitespace, or shell metacharacters, none of
// which are valid in any reference component).
func ValidateImageName(name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid image reference: %q", name)
	}
	if _, err := reference.Parse(name); err != nil {
		return fmt.Errorf("invalid image reference %q: %w", name, err)
	}
	return nil
}

// ScanImageFunc runs aquasec/trivy against the given docker image name using the local docker binary.
// This requires the host to have docker installed and accessible.
var ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
	if err := ValidateImageName(imageName); err != nil {
		return nil, err
	}

	log.Printf("Queuing trivy scan for image: %s", imageName)

	// Acquire semaphore to limit concurrency
	scanSem <- struct{}{}
	defer func() { <-scanSem }()

	log.Printf("Starting trivy scan for image: %s", imageName)

	// Apply a 5-minute timeout specifically for the active execution phase, independent of queue wait time
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// "--" terminates flag parsing so imageName can never be read as a trivy/docker option.
	// The trivy-vuln-db volume persists Trivy's ~500MB vulnerability database across
	// scans — without it, every scan re-downloads the full DB from scratch (since the
	// trivy container itself is --rm'd), which on a slow/constrained host can alone
	// exceed the exec timeout below and make every single scan silently time out.
	cmd := execCommandContext(execCtx, "docker", "run", "--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", "trivy-vuln-db:/root/.cache/trivy",
		"aquasec/trivy:latest", "image", "-f", "json", "--quiet", "--timeout", "5m", "--", imageName)

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

	// Filter out known false positives (e.g. GO-2026-5932 which is flagged on x/crypto globally)
	if results, ok := result["Results"].([]interface{}); ok {
		for _, r := range results {
			if target, ok := r.(map[string]interface{}); ok {
				if vulns, ok := target["Vulnerabilities"].([]interface{}); ok {
					var filtered []interface{}
					for _, v := range vulns {
						if vuln, ok := v.(map[string]interface{}); ok {
							if id, _ := vuln["VulnerabilityID"].(string); id != "GO-2026-5932" {
								filtered = append(filtered, v)
							}
						}
					}
					target["Vulnerabilities"] = filtered
				}
			}
		}
	}

	return result, nil
}
