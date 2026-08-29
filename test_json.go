package main

import (
	"encoding/json"
	"fmt"
	"github.com/moby/moby/api/types"
	"github.com/moby/moby/api/types/mount"
)

func main() {
	c := types.Container{
		Mounts: []types.MountPoint{
			{
				Type: mount.TypeVolume,
				Name: "my-vol",
			},
		},
	}
	b, _ := json.Marshal(c)
	fmt.Println(string(b))
}
