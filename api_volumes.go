package main

import (
	"context"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
)

// RegisterVolumeRoutes wires up the /api/volumes endpoints: listing,
// deleting, and pruning Docker volumes.
func RegisterVolumeRoutes(r *echo.Group, cli *client.Client) {
	r.GET("/volumes", handleGETVolumes(cli))
	r.DELETE("/volumes/:name", handleDELETEVolumesName(cli))
	r.POST("/volumes/prune", handlePOSTVolumesPrune(cli))
}

// handleGETVolumes lists all Docker volumes.
func handleGETVolumes(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		volumes, err := cli.VolumeList(context.Background(), client.VolumeListOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, volumes)
	}
}

// handleDELETEVolumesName force-removes one named volume. Requires admin or
// the can_delete permission.
func handleDELETEVolumesName(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDelete {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "You do not have permission to delete or prune resources."})
		}

		name := c.Param("name")
		_, err := cli.VolumeRemove(context.Background(), name, client.VolumeRemoveOptions{Force: true})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		logAudit(userClaims.ID, userClaims.Username, "DELETE", "Volume:"+name, "Success", "Deleted volume")

		return c.JSON(http.StatusOK, map[string]string{"message": "Volume deleted successfully"})
	}
}

// handlePOSTVolumesPrune removes all volumes not referenced by any container.
func handlePOSTVolumesPrune(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDelete {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "You do not have permission to delete or prune resources."})
		}

		var req struct {
			RemoveContainers bool `json:"remove_containers"`
		}
		_ = c.Bind(&req)

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

		res, err := cli.VolumePrune(context.Background(), client.VolumePruneOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		logAudit(userClaims.ID, userClaims.Username, "PRUNE", "Volumes", "Success", "Pruned unused volumes")

		return c.JSON(http.StatusOK, map[string]interface{}{
			"Report":  res,
			"Warning": warning,
		})
	}
}
