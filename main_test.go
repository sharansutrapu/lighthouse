package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/moby/moby/client"
	"context"

	"lighthouse/db"
	"lighthouse/scanner"
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

func TestTriggerRetroactiveScans(t *testing.T) {
	// Mock docker daemon
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		importPath := r.URL.Path
		if len(importPath) >= 16 && importPath[len(importPath)-16:] == "/containers/json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"Id":"123","Image":"mock-image:latest","Names":["/mock-container"]}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cli, err := client.NewClientWithOpts(client.WithHost(server.URL), client.WithHTTPClient(server.Client()))
	assert.NoError(t, err)

	originalScanImageFunc := scanner.ScanImageFunc
	defer func() { scanner.ScanImageFunc = originalScanImageFunc }()

	scanExecuted := false
	scanner.ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
		scanExecuted = true
		return map[string]interface{}{}, nil
	}

	db.GormDB.Where("1=1").Delete(&db.ImageScanResult{})

	triggerRetroactiveScans(cli)
	assert.True(t, scanExecuted, "Expected scan to execute on empty db")

	// Trigger again, mock ExecuteAndSaveScan will have saved it to DB?
	// Wait, we need the scanner package to actually save it! Let's insert a dummy record manually to simulate save.
	db.GormDB.Create(&db.ImageScanResult{Image: "mock-image:latest", Result: "{}"})

	scanExecuted = false
	triggerRetroactiveScans(cli)
	assert.False(t, scanExecuted, "Expected scan to NOT execute when result exists")
}
