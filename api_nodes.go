package main

import (
	"net/http"
	"time"

	"lighthouse/cluster"

	"github.com/labstack/echo/v4"
)

type nodeResponse struct {
	ID             string    `json:"id"`
	Role           string    `json:"role"`
	Connected      bool      `json:"connected"`
	LastSeen       time.Time `json:"last_seen"`
	ContainerCount int       `json:"container_count"`
	Capabilities   []string  `json:"capabilities"`
}

// handleGETNodes returns the hub and every spoke seen during this process.
// It is registered under the live-admin middleware in main.
func handleGETNodes() echo.HandlerFunc {
	return func(c echo.Context) error {
		globalContainerListMu.RLock()
		localContainerCount := globalContainersCount
		globalContainerListMu.RUnlock()

		nodes := []nodeResponse{
			{
				ID:             NodeID,
				Role:           LighthouseMode,
				Connected:      true,
				LastSeen:       time.Now(),
				ContainerCount: localContainerCount,
				Capabilities:   []string{"inspect", "logs", "shell", "stats", "actions", "scan", "gitops"},
			},
		}

		if LighthouseMode == "hub" {
			for _, spoke := range cluster.SnapshotSpokes() {
				nodes = append(nodes, nodeResponse{
					ID:             spoke.ID,
					Role:           "spoke",
					Connected:      spoke.Connected,
					LastSeen:       spoke.LastSeen,
					ContainerCount: spoke.ContainerCount,
					Capabilities:   spoke.Capabilities,
				})
			}
		}

		return c.JSON(http.StatusOK, nodes)
	}
}
