package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lighthouse/db"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
)

// RegisterRegistryRoutes wires up Private Registry Management endpoints.
func RegisterRegistryRoutes(r *echo.Group, cli *client.Client) {
	// GET /registries - list all configured registries
	r.GET("/registries", handleGETRegistries())

	// POST /registries - add a new registry
	r.POST("/registries", handlePOSTRegistries())

	// PUT /registries/:id - update existing registry
	r.PUT("/registries/:id", handlePUTRegistries())

	// DELETE /registries/:id - remove registry
	r.DELETE("/registries/:id", handleDELETERegistries())

	// POST /images/pull - authenticated image pull
	r.POST("/images/pull", handlePOSTImagePull(cli))
}

type RegistryView struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Username    string    `json:"username"`
	HasPassword bool      `json:"has_password"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func handleGETRegistries() echo.HandlerFunc {
	return func(c echo.Context) error {
		var registries []db.Registry
		if err := db.GormDB.Order("created_at asc").Find(&registries).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch registries"})
		}

		var views []RegistryView
		for _, reg := range registries {
			views = append(views, RegistryView{
				ID:          reg.ID,
				Name:        reg.Name,
				URL:         reg.URL,
				Username:    reg.Username,
				HasPassword: reg.Password != "",
				IsDefault:   reg.IsDefault,
				CreatedAt:   reg.CreatedAt,
				UpdatedAt:   reg.UpdatedAt,
			})
		}
		if views == nil {
			views = []RegistryView{}
		}
		return c.JSON(http.StatusOK, views)
	}
}

func handlePOSTRegistries() echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin permission required"})
		}

		var req struct {
			Name      string `json:"name"`
			URL       string `json:"url"`
			Username  string `json:"username"`
			Password  string `json:"password"`
			IsDefault bool   `json:"is_default"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		}

		req.Name = strings.TrimSpace(req.Name)
		req.URL = strings.TrimSpace(req.URL)
		if req.Name == "" || req.URL == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Name and registry URL are required"})
		}

		reg := db.Registry{
			Name:      req.Name,
			URL:       req.URL,
			Username:  req.Username,
			Password:  req.Password,
			IsDefault: req.IsDefault,
		}

		if err := db.GormDB.Create(&reg).Error; err != nil {
			return c.JSON(http.StatusConflict, map[string]string{"error": "Registry with this name already exists"})
		}

		logAudit(userClaims.ID, userClaims.Username, "CREATE", "Registry:"+reg.Name, "Success", "Added private registry")
		return c.JSON(http.StatusCreated, RegistryView{
			ID:          reg.ID,
			Name:        reg.Name,
			URL:         reg.URL,
			Username:    reg.Username,
			HasPassword: reg.Password != "",
			IsDefault:   reg.IsDefault,
			CreatedAt:   reg.CreatedAt,
			UpdatedAt:   reg.UpdatedAt,
		})
	}
}

func handlePUTRegistries() echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin permission required"})
		}

		id := c.Param("id")
		var reg db.Registry
		if err := db.GormDB.First(&reg, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Registry not found"})
		}

		var req struct {
			Name      string `json:"name"`
			URL       string `json:"url"`
			Username  string `json:"username"`
			Password  string `json:"password"`
			IsDefault bool   `json:"is_default"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		}

		if req.Name != "" {
			reg.Name = strings.TrimSpace(req.Name)
		}
		if req.URL != "" {
			reg.URL = strings.TrimSpace(req.URL)
		}
		reg.Username = req.Username
		if req.Password != "" {
			reg.Password = req.Password
		}
		reg.IsDefault = req.IsDefault

		if err := db.GormDB.Save(&reg).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update registry"})
		}

		logAudit(userClaims.ID, userClaims.Username, "UPDATE", "Registry:"+reg.Name, "Success", "Updated registry credentials")
		return c.JSON(http.StatusOK, RegistryView{
			ID:          reg.ID,
			Name:        reg.Name,
			URL:         reg.URL,
			Username:    reg.Username,
			HasPassword: reg.Password != "",
			IsDefault:   reg.IsDefault,
			CreatedAt:   reg.CreatedAt,
			UpdatedAt:   reg.UpdatedAt,
		})
	}
}

func handleDELETERegistries() echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Admin permission required"})
		}

		id := c.Param("id")
		var reg db.Registry
		if err := db.GormDB.First(&reg, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Registry not found"})
		}

		if err := db.GormDB.Delete(&reg).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete registry"})
		}

		logAudit(userClaims.ID, userClaims.Username, "DELETE", "Registry:"+reg.Name, "Success", "Deleted registry")
		return c.JSON(http.StatusOK, map[string]string{"message": "Registry deleted successfully"})
	}
}

func handlePOSTImagePull(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanStart && !userClaims.CanCreateDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		var req struct {
			Image      string `json:"image"`
			RegistryID uint   `json:"registry_id"`
		}
		if err := c.Bind(&req); err != nil || strings.TrimSpace(req.Image) == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Image name is required"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Minute)
		defer cancel()

		pullOptions := client.ImagePullOptions{}

		// Lookup registry credentials if ID provided or auto-match domain
		var targetReg *db.Registry
		if req.RegistryID > 0 {
			var r db.Registry
			if err := db.GormDB.First(&r, req.RegistryID).Error; err == nil {
				targetReg = &r
			}
		} else {
			// Try auto-match by image host
			var registries []db.Registry
			db.GormDB.Find(&registries)
			for _, r := range registries {
				cleanURL := r.URL
				if parsed, err := url.Parse(r.URL); err == nil && parsed.Host != "" {
					cleanURL = parsed.Host
				}
				if strings.HasPrefix(req.Image, cleanURL) {
					targetReg = &r
					break
				}
			}
		}

		if targetReg != nil && targetReg.Username != "" {
			authConfig := registry.AuthConfig{
				Username:      targetReg.Username,
				Password:      targetReg.Password,
				ServerAddress: targetReg.URL,
			}
			encodedJSON, err := json.Marshal(authConfig)
			if err == nil {
				pullOptions.RegistryAuth = base64.URLEncoding.EncodeToString(encodedJSON)
			}
		}

		reader, err := cli.ImagePull(ctx, req.Image, pullOptions)
		if err != nil {
			logAudit(userClaims.ID, userClaims.Username, "PULL_FAILED", "Image:"+req.Image, "Failed", err.Error())
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to pull image: %v", err)})
		}
		defer reader.Close()

		// Read output to completion
		outBytes, _ := io.ReadAll(reader)

		logAudit(userClaims.ID, userClaims.Username, "PULL", "Image:"+req.Image, "Success", "Pulled image successfully")
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message": "Image pulled successfully",
			"image":   req.Image,
			"details": string(outBytes),
		})
	}
}
