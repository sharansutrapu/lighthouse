package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func setupTestDB() {
	db.InitDB(":memory:")
}

func TestRegisterApiTokenRoutes(t *testing.T) {
	setupTestDB()

	// GET /tokens success
	t.Run("GetTokens_Success", func(t *testing.T) {
		setupTestDB()
		db.GormDB.Create(&db.ApiToken{UserID: 1, Name: "token1", Token: "lh_pat_xxx"})

		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodGet
		c.Request().URL.Path = "/tokens"
		err := handleGETTokens()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "token1")
	})

	// GET /tokens empty
	t.Run("GetTokens_Empty", func(t *testing.T) {
		setupTestDB()
		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodGet
		c.Request().URL.Path = "/tokens"
		err := handleGETTokens()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "[]\n", rec.Body.String())
	})

	// GET /tokens db error
	t.Run("GetTokens_DBError", func(t *testing.T) {
		setupTestDB()
		db.GormDB.Exec("DROP TABLE api_tokens") // force error
		defer db.GormDB.AutoMigrate(&db.ApiToken{})
		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodGet
		c.Request().URL.Path = "/tokens"
		err := handleGETTokens()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to fetch tokens")
	})

	// POST /tokens invalid payload
	t.Run("PostTokens_InvalidPayload", func(t *testing.T) {
		setupTestDB()
		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		req := httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewBufferString("{invalid_json"))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c.SetRequest(req)
		err := handlePOSTTokens()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "Invalid request payload")
	})

	// POST /tokens empty name
	t.Run("PostTokens_EmptyName", func(t *testing.T) {
		setupTestDB()
		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		req := httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewBufferString(`{"name": ""}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c.SetRequest(req)
		err := handlePOSTTokens()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "Name is required")
	})

	// POST /tokens db create error
	t.Run("PostTokens_DBError", func(t *testing.T) {
		setupTestDB()
		db.GormDB.Exec("DROP TABLE api_tokens") // force error
		defer db.GormDB.AutoMigrate(&db.ApiToken{})
		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		req := httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewBufferString(`{"name": "test_token"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c.SetRequest(req)
		err := handlePOSTTokens()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to create token")
	})

	// POST /tokens success
	t.Run("PostTokens_Success", func(t *testing.T) {
		setupTestDB()
		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		req := httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewBufferString(`{"name": "test_token"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c.SetRequest(req)
		err := handlePOSTTokens()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "test_token")
		assert.Contains(t, rec.Body.String(), "lh_pat_")
	})

	// DELETE /tokens DB error
	t.Run("DeleteTokens_DBError", func(t *testing.T) {
		setupTestDB()
		db.GormDB.Exec("DROP TABLE api_tokens") // force error
		defer db.GormDB.AutoMigrate(&db.ApiToken{})
		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodDelete
		c.Request().URL.Path = "/tokens/1"
		c.SetParamNames("id")
		c.SetParamValues("1")

		err := handleDELETETokensId()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "Failed to delete token")
	})

	// DELETE /tokens success
	t.Run("DeleteTokens_Success", func(t *testing.T) {
		setupTestDB()
		db.GormDB.Create(&db.ApiToken{ID: 1, UserID: 1, Name: "test_token", Token: "lh_pat_xxx"})
		claims := &UserClaims{ID: 1, Username: "admin"}
		_, _, c, rec := setupEchoWithClaimsHelper(claims)

		c.Request().Method = http.MethodDelete
		c.Request().URL.Path = "/tokens/1"
		c.SetParamNames("id")
		c.SetParamValues("1")

		err := handleDELETETokensId()(c)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Token deleted")

		// Verify deleted
		var count int64
		db.GormDB.Model(&db.ApiToken{}).Count(&count)
		assert.Equal(t, int64(0), count)
	})
}


func TestRegisterApiTokenRoutes_Coverage(t *testing.T) {
	e := echo.New()
	g := e.Group("/api")
	registerApiTokenRoutes(g)
	assert.NotNil(t, g)
}
