package main

import (
	"context"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
)

func RegisterImageRoutes(r *echo.Group, cli *client.Client) {
	r.GET("/images", func(c echo.Context) error {
		images, err := cli.ImageList(context.Background(), client.ImageListOptions{All: true})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, images)
	})

	r.DELETE("/images/:id", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDelete {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "You do not have permission to delete or prune resources."})
		}

		id := c.Param("id")
		res, err := cli.ImageRemove(context.Background(), id, client.ImageRemoveOptions{Force: true, PruneChildren: true})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		logAudit(userClaims.ID, userClaims.Username, "DELETE", "Image:"+id, "Success", "Deleted image")

		return c.JSON(http.StatusOK, res)
	})

	r.POST("/images/prune", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDelete {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "You do not have permission to delete or prune resources."})
		}

		res, err := cli.ImagePrune(context.Background(), client.ImagePruneOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		
		logAudit(userClaims.ID, userClaims.Username, "PRUNE", "Images", "Success", "Pruned unused images")

		return c.JSON(http.StatusOK, res)
	})
}
