package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
)

// RegisterSystemPruneRoutes registers disk usage and system clean-up wizard endpoints.
func RegisterSystemPruneRoutes(r *echo.Group, cli *client.Client) {
	// GET /system/df - detailed disk usage and reclaimable space breakdown
	r.GET("/system/df", handleGETSystemDiskUsage(cli))

	// POST /system/prune - execute multi-target prune operation
	r.POST("/system/prune", handlePOSTSystemPrune(cli))
}

type SystemDiskUsageResponse struct {
	LayersSize            int64 `json:"layers_size"`
	ImagesCount           int   `json:"images_count"`
	ImagesSize            int64 `json:"images_size"`
	ImagesReclaimable     int64 `json:"images_reclaimable"`
	ContainersCount       int   `json:"containers_count"`
	ContainersSize        int64 `json:"containers_size"`
	VolumesCount          int   `json:"volumes_count"`
	VolumesSize           int64 `json:"volumes_size"`
	VolumesReclaimable    int64 `json:"volumes_reclaimable"`
	BuildCacheCount       int   `json:"build_cache_count"`
	BuildCacheSize        int64 `json:"build_cache_size"`
	BuildCacheReclaimable int64 `json:"build_cache_reclaimable"`
	TotalReclaimable      int64 `json:"total_reclaimable"`
}

func handleGETSystemDiskUsage(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 20*time.Second)
		defer cancel()

		usage, err := cli.DiskUsage(ctx, client.DiskUsageOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to inspect disk usage: %v", err)})
		}

		resp := SystemDiskUsageResponse{
			ImagesCount:           int(usage.Images.TotalCount),
			ImagesSize:            usage.Images.TotalSize,
			ImagesReclaimable:     usage.Images.Reclaimable,
			ContainersCount:       int(usage.Containers.TotalCount),
			ContainersSize:        usage.Containers.TotalSize,
			VolumesCount:          int(usage.Volumes.TotalCount),
			VolumesSize:           usage.Volumes.TotalSize,
			VolumesReclaimable:    usage.Volumes.Reclaimable,
			BuildCacheCount:       int(usage.BuildCache.TotalCount),
			BuildCacheSize:        usage.BuildCache.TotalSize,
			BuildCacheReclaimable: usage.BuildCache.Reclaimable,
			TotalReclaimable:      usage.Images.Reclaimable + usage.Volumes.Reclaimable + usage.BuildCache.Reclaimable,
		}

		return c.JSON(http.StatusOK, resp)
	}
}

type SystemPruneRequest struct {
	PruneImages     bool `json:"prune_images"`
	PruneContainers bool `json:"prune_containers"`
	PruneVolumes    bool `json:"prune_volumes"`
	PruneNetworks   bool `json:"prune_networks"`
	PruneBuildCache bool `json:"prune_build_cache"`
	All             bool `json:"all"` // if true, prunes all unused images, not just dangling
}

type SystemPruneResponse struct {
	SpaceReclaimedBytes uint64   `json:"space_reclaimed_bytes"`
	ContainersDeleted   []string `json:"containers_deleted"`
	ImagesDeleted       []string `json:"images_deleted"`
	VolumesDeleted      []string `json:"volumes_deleted"`
	NetworksDeleted     []string `json:"networks_deleted"`
	BuildCacheReclaimed uint64   `json:"build_cache_reclaimed"`
}

func handlePOSTSystemPrune(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDelete {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin or delete permission required to prune system resources"})
		}

		var req SystemPruneRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid prune request"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Minute)
		defer cancel()

		resp := SystemPruneResponse{
			ContainersDeleted: []string{},
			ImagesDeleted:     []string{},
			VolumesDeleted:    []string{},
			NetworksDeleted:   []string{},
		}

		// 1. Prune stopped containers
		if req.PruneContainers {
			cReport, err := cli.ContainerPrune(ctx, client.ContainerPruneOptions{})
			if err == nil {
				resp.ContainersDeleted = cReport.Report.ContainersDeleted
				resp.SpaceReclaimedBytes += cReport.Report.SpaceReclaimed
			}
		}

		// 2. Prune images
		if req.PruneImages {
			pruneFilters := make(client.Filters)
			if !req.All {
				pruneFilters.Add("dangling", "true")
			}
			iReport, err := cli.ImagePrune(ctx, client.ImagePruneOptions{Filters: pruneFilters})
			if err == nil {
				for _, img := range iReport.Report.ImagesDeleted {
					if img.Deleted != "" {
						resp.ImagesDeleted = append(resp.ImagesDeleted, img.Deleted)
					} else if img.Untagged != "" {
						resp.ImagesDeleted = append(resp.ImagesDeleted, img.Untagged)
					}
				}
				resp.SpaceReclaimedBytes += iReport.Report.SpaceReclaimed
			}
		}

		// 3. Prune volumes
		if req.PruneVolumes {
			vReport, err := cli.VolumePrune(ctx, client.VolumePruneOptions{})
			if err == nil {
				resp.VolumesDeleted = vReport.Report.VolumesDeleted
				resp.SpaceReclaimedBytes += vReport.Report.SpaceReclaimed
			}
		}

		// 4. Prune unused networks
		if req.PruneNetworks {
			nReport, err := cli.NetworkPrune(ctx, client.NetworkPruneOptions{})
			if err == nil {
				resp.NetworksDeleted = nReport.Report.NetworksDeleted
			}
		}

		// 5. Prune build cache
		if req.PruneBuildCache {
			bcReport, err := cli.BuildCachePrune(ctx, client.BuildCachePruneOptions{})
			if err == nil {
				resp.BuildCacheReclaimed = bcReport.Report.SpaceReclaimed
				resp.SpaceReclaimedBytes += bcReport.Report.SpaceReclaimed
			}
		}

		logAudit(userClaims.ID, userClaims.Username, "SYSTEM_PRUNE", "System", "Success", fmt.Sprintf("Reclaimed %d bytes of disk space", resp.SpaceReclaimedBytes))

		return c.JSON(http.StatusOK, resp)
	}
}
