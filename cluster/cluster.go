package cluster

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"lighthouse/db"
)

// ProxyRequest forwards an HTTP request to a Spoke node and returns the JSON response
func ProxyRequest(node db.Node, method, endpoint string, body io.Reader) ([]byte, error) {
	url := strings.TrimRight(node.Address, "/") + endpoint
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+node.Token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("spoke error (%d): %s", resp.StatusCode, string(b))
	}

	return io.ReadAll(resp.Body)
}

// FetchSpokeContainers fetches container list from a specific Spoke
func FetchSpokeContainers(node db.Node) ([]map[string]interface{}, error) {
	b, err := ProxyRequest(node, "GET", "/api/containers", nil)
	if err != nil {
		return nil, err
	}

	var containers []map[string]interface{}
	if err := json.Unmarshal(b, &containers); err != nil {
		return nil, err
	}

	// Tag each container with the NodeID
	for i := range containers {
		containers[i]["node_id"] = node.ID
		containers[i]["node_name"] = node.Name
	}
	return containers, nil
}
