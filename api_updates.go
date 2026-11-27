package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"lighthouse/db"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
)

// RegisterUpdateRoutes registers Watchtower & Canary Auto-Update endpoints.
func RegisterUpdateRoutes(r *echo.Group, cli *client.Client) {
	// GET /updates/check - inspect running containers for available image updates
	r.GET("/updates/check", handleGETCheckUpdates(cli))

	// POST /updates/apply - 1-click update with canary healthcheck rollback
	r.POST("/updates/apply", handlePOSTApplyUpdateWithRollback(cli))

	// GET /updates/history - view historical update & rollback events
	r.GET("/updates/history", handleGETUpdateHistory())
}

type UpdateCheckItem struct {
	ContainerID     string `json:"container_id"`
	ContainerName   string `json:"container_name"`
	Image           string `json:"image"`
	CurrentImageID  string `json:"current_image_id"`
	UpdateAvailable bool   `json:"update_available"`
}

func handleGETCheckUpdates(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Second)
		defer cancel()

		containersRes, err := cli.ContainerList(ctx, client.ContainerListOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to list containers"})
		}

		var items []UpdateCheckItem
		for _, ctr := range containersRes.Items {
			name := ""
			if len(ctr.Names) > 0 {
				name = strings.TrimPrefix(ctr.Names[0], "/")
			}
			// Skip lighthouse container self
			if isLightHouseSelfContainer(name, ctr.Image) {
				continue
			}

			item := UpdateCheckItem{
				ContainerID:     ctr.ID[:12],
				ContainerName:   name,
				Image:           ctr.Image,
				CurrentImageID:  ctr.ImageID,
				UpdateAvailable: false,
			}

			// In remote mode, pull latest tag to inspect image ID
			if c.QueryParam("remote") == "true" {
				reader, pullErr := cli.ImagePull(ctx, ctr.Image, client.ImagePullOptions{})
				if pullErr == nil {
					_, _ = io.Copy(io.Discard, reader)
					_ = reader.Close()

					inspect, inspectErr := cli.ImageInspect(ctx, ctr.Image)
					if inspectErr == nil && inspect.ID != ctr.ImageID {
						item.UpdateAvailable = true
					}
				}
			}

			items = append(items, item)
		}

		if items == nil {
			items = []UpdateCheckItem{}
		}
		return c.JSON(http.StatusOK, items)
	}
}

func handlePOSTApplyUpdateWithRollback(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanStart && !userClaims.CanRestart {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		var req struct {
			ContainerID string `json:"container_id"`
		}
		if err := c.Bind(&req); err != nil || req.ContainerID == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Container ID is required"})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// 1. Inspect target container
		inspect, err := cli.ContainerInspect(ctx, req.ContainerID, client.ContainerInspectOptions{})
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Target container not found"})
		}

		containerName := strings.TrimPrefix(inspect.Container.Name, "/")
		imageName := inspect.Container.Config.Image
		if isLightHouseSelfContainer(containerName, imageName) {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Cannot update the LightHouse container itself"})
		}

		originalImageID := inspect.Container.Image

		// 2. Pull latest image
		pullReader, err := cli.ImagePull(ctx, imageName, client.ImagePullOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed pulling latest image %s: %v", imageName, err)})
		}
		_, _ = io.Copy(io.Discard, pullReader)
		_ = pullReader.Close()

		newImgInspect, err := cli.ImageInspect(ctx, imageName)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed inspecting newly pulled image"})
		}

		if newImgInspect.ID == originalImageID {
			return c.JSON(http.StatusOK, map[string]interface{}{
				"status":  "up_to_date",
				"message": "Container is already running the latest image digest",
			})
		}

		// 3. Stop old container
		stopTimeout := 15
		_, _ = cli.ContainerStop(ctx, inspect.Container.ID, client.ContainerStopOptions{Timeout: &stopTimeout})

		// 4. Rename old container to backup
		backupName := fmt.Sprintf("%s_rollback_bak_%d", containerName, time.Now().Unix())
		if _, err := cli.ContainerRename(ctx, inspect.Container.ID, client.ContainerRenameOptions{NewName: backupName}); err != nil {
			_, _ = cli.ContainerStart(ctx, inspect.Container.ID, client.ContainerStartOptions{})
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to stage backup container"})
		}

		// 5. Create new container with identical configuration and latest image
		createOpts := client.ContainerCreateOptions{
			Config:     inspect.Container.Config,
			HostConfig: inspect.Container.HostConfig,
			Name:       containerName,
		}
		createResp, err := cli.ContainerCreate(ctx, createOpts)
		if err != nil {
			// Rollback rename & restart old container
			_, _ = cli.ContainerRename(ctx, inspect.Container.ID, client.ContainerRenameOptions{NewName: containerName})
			_, _ = cli.ContainerStart(ctx, inspect.Container.ID, client.ContainerStartOptions{})
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to create new container: %v", err)})
		}

		// 6. Start new container
		if _, err := cli.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{}); err != nil {
			// Rollback!
			_, _ = cli.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true})
			_, _ = cli.ContainerRename(ctx, inspect.Container.ID, client.ContainerRenameOptions{NewName: containerName})
			_, _ = cli.ContainerStart(ctx, inspect.Container.ID, client.ContainerStartOptions{})

			db.GormDB.Create(&db.UpdateRollbackLog{
				ContainerName: containerName,
				OldImageID:    originalImageID[:12],
				NewImageID:    newImgInspect.ID[:12],
				Status:        "rolled_back",
				Message:       fmt.Sprintf("Failed to start new container: %v. Automatically rolled back.", err),
			})
			return c.JSON(http.StatusUnprocessableEntity, map[string]interface{}{
				"status":  "rolled_back",
				"message": "New container failed to start. Automatically rolled back to previous version.",
				"error":   err.Error(),
			})
		}

		// 7. Canary Healthcheck Verification (monitor for 5 seconds)
		time.Sleep(5 * time.Second)
		verifyInspect, err := cli.ContainerInspect(ctx, createResp.ID, client.ContainerInspectOptions{})
		isUnhealthy := false
		if err != nil || !verifyInspect.Container.State.Running {
			isUnhealthy = true
		} else if verifyInspect.Container.State.Health != nil && verifyInspect.Container.State.Health.Status == "unhealthy" {
			isUnhealthy = true
		}

		if isUnhealthy {
			// Trigger Automated Canary Rollback
			log.Printf("[Watchtower] Canary health check failed for %s. Executing automatic rollback...", containerName)
			stopT := 5
			_, _ = cli.ContainerStop(ctx, createResp.ID, client.ContainerStopOptions{Timeout: &stopT})
			_, _ = cli.ContainerRemove(ctx, createResp.ID, client.ContainerRemoveOptions{Force: true})
			_, _ = cli.ContainerRename(ctx, inspect.Container.ID, client.ContainerRenameOptions{NewName: containerName})
			_, _ = cli.ContainerStart(ctx, inspect.Container.ID, client.ContainerStartOptions{})

			db.GormDB.Create(&db.UpdateRollbackLog{
				ContainerName: containerName,
				OldImageID:    originalImageID[:12],
				NewImageID:    newImgInspect.ID[:12],
				Status:        "rolled_back",
				Message:       "Canary health check failed or container crashed. Rolled back successfully.",
			})

			logAudit(userClaims.ID, userClaims.Username, "UPDATE_ROLLBACK", "Container:"+containerName, "Rollback", "Canary check failed, rolled back to original image")
			return c.JSON(http.StatusOK, map[string]interface{}{
				"status":  "rolled_back",
				"message": "New image crashed or failed health check during canary verification. Container was automatically rolled back safely.",
			})
		}

		// Success! Safe to clean up the backup container
		_, _ = cli.ContainerRemove(ctx, inspect.Container.ID, client.ContainerRemoveOptions{Force: true})

		db.GormDB.Create(&db.UpdateRollbackLog{
			ContainerName: containerName,
			OldImageID:    originalImageID[:12],
			NewImageID:    newImgInspect.ID[:12],
			Status:        "success",
			Message:       "Container updated and verified healthy.",
		})

		logAudit(userClaims.ID, userClaims.Username, "UPDATE_APPLIED", "Container:"+containerName, "Success", fmt.Sprintf("Updated image to %s (%s)", imageName, newImgInspect.ID[:12]))
		return c.JSON(http.StatusOK, map[string]interface{}{
			"status":       "success",
			"message":      "Container updated successfully and verified healthy",
			"container_id": createResp.ID[:12],
			"image":        imageName,
		})
	}
}

func handleGETUpdateHistory() echo.HandlerFunc {
	return func(c echo.Context) error {
		var logs []db.UpdateRollbackLog
		db.GormDB.Order("timestamp desc").Limit(100).Find(&logs)
		if logs == nil {
			logs = []db.UpdateRollbackLog{}
		}
		return c.JSON(http.StatusOK, logs)
	}
}
