package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"lighthouse/cluster"
)

func aggregateClusterResources(ctx context.Context, action string, local interface{}) []map[string]interface{} {
	items := tagResourceItems(resourceItems(local), NodeID, false)
	if LighthouseMode != "hub" {
		return items
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, nodeID := range cluster.ConnectedSpokeIDsWithCapability("resources") {
		nodeID := nodeID
		wg.Add(1)
		go func() {
			defer wg.Done()
			requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			data, err := cluster.CallSpoke(requestCtx, nodeID, action, nil)
			if err != nil {
				return
			}
			var decoded interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return
			}
			remoteItems := tagResourceItems(resourceItems(decoded), nodeID, true)
			mu.Lock()
			items = append(items, remoteItems...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return items
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
