package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"lighthouse/db"
)

func generateApiTokenString() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "lh_pat_" + hex.EncodeToString(b)
}

func registerApiTokenRoutes(r *echo.Group) {
	r.GET("/tokens", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)

		var tokens []db.ApiToken
		if err := db.GormDB.Where("user_id = ?", claims.ID).Order("created_at desc").Find(&tokens).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch tokens"})
		}

		type tokenResponse struct {
			ID        uint      `json:"id"`
			Name      string    `json:"name"`
			CreatedAt time.Time `json:"created_at"`
			LastUsed  time.Time `json:"last_used"`
		}
		
		var res []tokenResponse
		for _, t := range tokens {
			res = append(res, tokenResponse{
				ID:        t.ID,
				Name:      t.Name,
				CreatedAt: t.CreatedAt,
				LastUsed:  t.LastUsed,
			})
		}
		if res == nil {
			res = []tokenResponse{}
		}

		return c.JSON(http.StatusOK, res)
	})

	r.POST("/tokens", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)

		var req struct {
			Name string `json:"name"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		}
		if req.Name == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Name is required"})
		}

		apiToken := db.ApiToken{
			UserID:   uint(claims.ID),
			Name:     req.Name,
			Token:    generateApiTokenString(),
		}

		if err := db.GormDB.Create(&apiToken).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create token"})
		}

		return c.JSON(http.StatusOK, apiToken)
	})

	r.DELETE("/tokens/:id", func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)
		id := c.Param("id")

		if err := db.GormDB.Where("id = ? AND user_id = ?", id, claims.ID).Delete(&db.ApiToken{}).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete token"})
		}

		return c.JSON(http.StatusOK, map[string]string{"message": "Token deleted"})
	})
}
