package main

import (
	"context"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
)

func RegisterVolumeRoutes(r *echo.Group, cli *client.Client) {
	r.GET("/volumes", func(c echo.Context) error {
		volumes, err := cli.VolumeList(context.Background(), client.VolumeListOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, volumes)
	})

	r.DELETE("/volumes/:name", func(c echo.Context) error {
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
	})

	r.POST("/volumes/prune", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDelete {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "You do not have permission to delete or prune resources."})
		}

		res, err := cli.VolumePrune(context.Background(), client.VolumePruneOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		
		logAudit(userClaims.ID, userClaims.Username, "PRUNE", "Volumes", "Success", "Pruned unused volumes")

		return c.JSON(http.StatusOK, res)
	})
}
