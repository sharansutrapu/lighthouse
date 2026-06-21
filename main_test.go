package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"lighthouse/db"
)

func TestMain(m *testing.M) {
	if err := db.InitDB(":memory:"); err != nil {
		panic("failed to init test db: " + err.Error())
	}
	os.Exit(m.Run())
}

func TestConfigRoute(t *testing.T) {
	e := echo.New()
	
	// Temporarily override global config values for the test
	originalCanStart := CanStart
	originalAllowShell := AllowShell
	CanStart = true
	AllowShell = false
	defer func() {
		CanStart = originalCanStart
		AllowShell = originalAllowShell
	}()

	e.GET("/api/config", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"allow_start":   CanStart,
			"allow_stop":    CanStop,
			"allow_restart": CanRestart,
			"allow_delete":  CanDelete,
			"allow_shell":   AllowShell,
			"client_access": clientAccessConfig(),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, true, response["allow_start"])
	assert.Equal(t, false, response["allow_shell"])
}
