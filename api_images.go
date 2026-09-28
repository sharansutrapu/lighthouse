package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"lighthouse/cluster"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
)

// RegisterImageRoutes wires up the /api/images endpoints: listing, deleting,
// and pruning Docker images. Delete/prune require the caller to be an admin
// or hold the can_delete permission.
func RegisterImageRoutes(r *echo.Group, cli *client.Client) {
	// GET /images lists all locally pulled images.
	r.GET("/images", func(c echo.Context) error {
		images, err := cli.ImageList(context.Background(), client.ImageListOptions{All: false})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if LighthouseMode == "hub" {
			return c.JSON(http.StatusOK, aggregateClusterResources(c.Request().Context(), "list_images", images))
		}
		return c.JSON(http.StatusOK, images)
	})

	// DELETE /images/:id force-removes one image (and any now-unused parent layers).
	r.DELETE("/images/:id", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDelete {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "You do not have permission to delete or prune resources."})
		}

		id := c.Param("id")
		nodeID := c.QueryParam("node_id")
		if LighthouseMode == "hub" && nodeID != "" && nodeID != NodeID {
			if !cluster.SpokeSupports(nodeID, "resources") {
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Spoke upgrade required for remote resource management"})
			}
			ctx, cancel := context.WithTimeout(c.Request().Context(), 90*time.Second)
			defer cancel()
			data, err := cluster.CallSpoke(ctx, nodeID, "image_remove", map[string]string{"id": id})
			if err != nil {
				return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
			logAudit(userClaims.ID, userClaims.Username, "DELETE", "Image:"+id, "Success", "Deleted image on "+nodeID)
			return c.JSONBlob(http.StatusOK, data)
		}
		res, err := cli.ImageRemove(context.Background(), id, client.ImageRemoveOptions{Force: true, PruneChildren: true})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		logAudit(userClaims.ID, userClaims.Username, "DELETE", "Image:"+id, "Success", "Deleted image")

		return c.JSON(http.StatusOK, res)
	})

	// POST /images/prune removes dangling (or, if requested, all unused) images.
	r.POST("/images/prune", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDelete {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "You do not have permission to delete or prune resources."})
		}

		var req struct {
			AllUnused        bool `json:"all_unused"`
			RemoveContainers bool `json:"remove_containers"`
		}
		_ = c.Bind(&req)

		nodeID := c.QueryParam("node_id")
		if LighthouseMode == "hub" && nodeID != "" && nodeID != NodeID {
			if !cluster.SpokeSupports(nodeID, "resources") {
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Spoke upgrade required for remote resource management"})
			}
			ctx, cancel := context.WithTimeout(c.Request().Context(), 90*time.Second)
			defer cancel()
			data, err := cluster.CallSpoke(ctx, nodeID, "image_prune", req)
			if err != nil {
				return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
			logAudit(userClaims.ID, userClaims.Username, "PRUNE", "Images", "Success", "Pruned unused images on "+nodeID)
			return c.JSONBlob(http.StatusOK, data)
		}

		warning := ""
		if req.RemoveContainers {
			// First, prune containers to release any held resources
			cli.ContainerPrune(context.Background(), client.ContainerPruneOptions{})
		} else {
			// Check if there are stopped containers that might be holding onto resources
			stoppedFilters := make(client.Filters)
			stoppedFilters.Add("status", "exited", "created")
			stopped, _ := cli.ContainerList(context.Background(), client.ContainerListOptions{
				All:     true,
				Filters: stoppedFilters,
			})
			if len(stopped.Items) > 0 {
				warning = "Stopped containers detected. Some resources may not have been pruned."
			}
		}

		pruneFilters := make(client.Filters)
		if req.AllUnused {
			pruneFilters.Add("dangling", "false")
		} else {
			pruneFilters.Add("dangling", "true")
		}

		res, err := cli.ImagePrune(context.Background(), client.ImagePruneOptions{Filters: pruneFilters})
		if err != nil {
			log.Printf("ERROR: Docker ImagePrune failed: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		logAudit(userClaims.ID, userClaims.Username, "PRUNE", "Images", "Success", "Pruned unused images")

		return c.JSON(http.StatusOK, map[string]interface{}{
			"Report":  res,
			"Warning": warning,
		})
	})
}
