package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/moby/moby/client"
)

func main() {
	cli, _ := client.NewClientWithOpts(client.FromEnv)
	res, _ := cli.ContainerList(context.Background(), client.ContainerListOptions{All: true})
	b, _ := json.Marshal(res)
	
	var out []map[string]interface{}
	json.Unmarshal(b, &out)
	
	for i, c := range out {
		for k, _ := range c {
			if k == "Mounts" || k == "mounts" {
				b2, _ := json.Marshal(c[k])
				if string(b2) != "[]" && string(b2) != "null" {
					fmt.Printf("Container %d Key: %s Content: %s\n", i, k, string(b2))
				}
			}
		}
	}
}
