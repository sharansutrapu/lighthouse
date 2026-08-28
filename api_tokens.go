package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"lighthouse/db"
)

// generateApiTokenString creates a new random `lh_pat_<64 hex chars>` API
// token. These are long-lived, non-expiring credentials (unlike JWTs), used
// by scripts and MCP/AI agents to authenticate without a login session.
func generateApiTokenString() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "lh_pat_" + hex.EncodeToString(b)
}

// hashApiToken returns the SHA-256 hex digest of a token. API tokens are
// high-entropy random values (never user-chosen), so a fast cryptographic
// hash is sufficient — the risk being mitigated is DB-at-rest exposure
// (backup leak, insider read access), not brute-force guessing.
func hashApiToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// handleGETTokens lists the calling user's own API tokens (metadata only —
// the plaintext token value is never retrievable after creation).
func handleGETTokens() echo.HandlerFunc {
	return func(c echo.Context) error {
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
	}
}

// handlePOSTTokens creates a new API token for the calling user. The
// plaintext value is returned exactly once in this response; only its hash is
// ever stored, so losing this response means the token must be regenerated.
func handlePOSTTokens() echo.HandlerFunc {
	return func(c echo.Context) error {
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

		plaintext := generateApiTokenString()
		apiToken := db.ApiToken{
			UserID: uint(claims.ID),
			Name:   req.Name,
			// Only the hash is persisted; the plaintext is shown once below and never stored.
			Token: hashApiToken(plaintext),
		}

		if err := db.GormDB.Create(&apiToken).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create token"})
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"id":         apiToken.ID,
			"name":       apiToken.Name,
			"token":      plaintext,
			"created_at": apiToken.CreatedAt,
		})
	}
}

// handleDELETETokensId revokes one of the calling user's own API tokens.
func handleDELETETokensId() echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		claims := token.Claims.(*UserClaims)
		id := c.Param("id")

		if err := db.GormDB.Where("id = ? AND user_id = ?", id, claims.ID).Delete(&db.ApiToken{}).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete token"})
		}

		return c.JSON(http.StatusOK, map[string]string{"message": "Token deleted"})
	}
}

// registerApiTokenRoutes wires up the self-service /api/tokens endpoints
// users call to manage their own API tokens.
func registerApiTokenRoutes(r *echo.Group) {
	r.GET("/tokens", handleGETTokens())
	r.POST("/tokens", handlePOSTTokens())
	r.DELETE("/tokens/:id", handleDELETETokensId())
}
