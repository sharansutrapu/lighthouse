package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"lighthouse/cluster"
)

const unavailableNodesHeader = "X-LightHouse-Unavailable-Nodes"

func setUnavailableNodes(c echo.Context, nodes []string) {
	if len(nodes) == 0 {
		return
	}
	for index, nodeID := range nodes {
		if nodeID == "" {
			nodes[index] = "local"
		}
	}
	sort.Strings(nodes)
	encoded, _ := json.Marshal(nodes)
	c.Response().Header().Set(unavailableNodesHeader, string(encoded))
}

func writeClusterResources(c echo.Context, action string, local interface{}, localErr error) error {
	items, unavailable := aggregateClusterResources(c.Request().Context(), action, local)
	if localErr != nil {
		unavailable = append(unavailable, NodeID)
	}
	setUnavailableNodes(c, unavailable)
	return c.JSON(http.StatusOK, items)
}

func aggregateClusterResources(ctx context.Context, action string, local interface{}) ([]map[string]interface{}, []string) {
	items := tagResourceItems(resourceItems(local), NodeID, false)
	if LighthouseMode != "hub" {
		return items, nil
	}

	var unavailable []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, nodeID := range cluster.ConnectedSpokeIDs() {
		if !cluster.SpokeSupports(nodeID, "resources") {
			mu.Lock()
			unavailable = append(unavailable, nodeID)
			mu.Unlock()
			continue
		}
		nodeID := nodeID
		wg.Add(1)
		go func() {
			defer wg.Done()
			requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			data, err := cluster.CallSpoke(requestCtx, nodeID, action, nil)
			var decoded interface{}
			if err != nil || json.Unmarshal(data, &decoded) != nil {
				mu.Lock()
				unavailable = append(unavailable, nodeID)
				mu.Unlock()
				return
			}
			remoteItems := tagResourceItems(resourceItems(decoded), nodeID, true)
			mu.Lock()
			items = append(items, remoteItems...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return items, unavailable
}

func resourceItems(value interface{}) []map[string]interface{} {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil
	}
	return resourceItemsFromDecoded(decoded)
}

func resourceItemsFromDecoded(value interface{}) []map[string]interface{} {
	switch typed := value.(type) {
	case []interface{}:
		items := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			if resource, ok := item.(map[string]interface{}); ok {
				items = append(items, resource)
			}
		}
		return items
	case map[string]interface{}:
		for _, key := range []string{"Items", "Volumes", "Networks", "Images"} {
			if nested, ok := typed[key]; ok {
				if items := resourceItemsFromDecoded(nested); items != nil {
					return items
				}
			}
		}
		for _, nested := range typed {
			if items := resourceItemsFromDecoded(nested); items != nil {
				return items
			}
		}
	}
	return nil
}

func tagResourceItems(items []map[string]interface{}, nodeID string, remote bool) []map[string]interface{} {
	tagged := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		copy := make(map[string]interface{}, len(item)+2)
		for key, value := range item {
			copy[key] = value
		}
		copy["node_id"] = nodeID
		copy["is_remote"] = remote
		tagged = append(tagged, copy)
	}
	return tagged
}
