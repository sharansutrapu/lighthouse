package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"lighthouse/db"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
)

type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func mockDockerClient(transport *mockTransport) *client.Client {
	cli, _ := client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{Transport: transport}),
		client.WithVersion("1.41"),
	)
	return cli
}

func setupTestDB(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
}

func TestReloadSchedule(t *testing.T) {
	setupTestDB(t)

	// DB error
	db.GormDB.Migrator().DropTable(&db.Setting{})
	ReloadSchedule(nil)                  // should return early
	db.GormDB.AutoMigrate(&db.Setting{}) // restore

	db.GormDB.Save(&db.Setting{
		ID:                   1,
		ScheduledScanEnabled: false,
		ScheduledScanCron:    "0 0 * * *",
	})
	ReloadSchedule(nil)
	assert.Equal(t, 0, len(scheduler.Entries()))

	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("scheduled_scan_enabled", true)
	ReloadSchedule(nil)
	assert.Equal(t, 1, len(scheduler.Entries()))

	entryID := currentEntryID

	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("scheduled_scan_cron", "0 1 * * *")
	ReloadSchedule(nil)
	assert.Equal(t, 1, len(scheduler.Entries()))
	assert.NotEqual(t, entryID, currentEntryID)

	// invalid cron
	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("scheduled_scan_cron", "invalid")
	ReloadSchedule(nil)

	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("scheduled_scan_enabled", false)
	ReloadSchedule(nil)
	assert.Equal(t, 0, len(scheduler.Entries()))
}

func TestReloadSchedule_CronJobExecution(t *testing.T) {
	setupTestDB(t)

	mockT := &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("client error") // to safely return from RunScheduledScans
		},
	}
	cli := mockDockerClient(mockT)

	db.GormDB.Save(&db.Setting{
		ID:                   1,
		ScheduledScanEnabled: true,
		ScheduledScanCron:    "0 0 * * *",
	})
	ReloadSchedule(cli)
	assert.Equal(t, 1, len(scheduler.Entries()))

	// Execute the scheduled job manually to hit coverage
	scheduler.Entries()[0].Job.Run()
}

func TestRunScheduledScans(t *testing.T) {
	setupTestDB(t)

	mockT := &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/v1.41/containers/json" {
				containers := []container.Summary{
					{Image: "image1:latest"},
					{Image: ""}, // empty should skip
				}
				b, _ := json.Marshal(containers)
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(b)),
				}, nil
			}
			return nil, errors.New("not found")
		},
	}
	cli := mockDockerClient(mockT)

	originalScanImageFunc := ScanImageFunc
	defer func() { ScanImageFunc = originalScanImageFunc }()
	ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
		return map[string]interface{}{"success": true}, nil
	}

	RunScheduledScans(cli)

	var count int64
	db.GormDB.Model(&db.ImageScanResult{}).Count(&count)
	assert.Equal(t, int64(1), count)

	// Test client error
	mockT.roundTripFunc = func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("client error")
	}
	RunScheduledScans(cli) // should log error and return
}

func TestExecuteAndSaveScan(t *testing.T) {
	setupTestDB(t)

	originalScanImageFunc := ScanImageFunc
	defer func() { ScanImageFunc = originalScanImageFunc }()

	var alertTriggered bool
	AlertCallback = func(imageName string, resultBytes []byte) {
		alertTriggered = true
	}
	defer func() { AlertCallback = nil }()

	ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
		return map[string]interface{}{"result": "ok"}, nil
	}

	imageName := "test-image:latest"
	_, err := ExecuteAndSaveScan(context.Background(), nil, imageName)
	assert.NoError(t, err)

	var count int64
	db.GormDB.Model(&db.ImageScanResult{}).Where("image = ?", imageName).Count(&count)
	assert.Equal(t, int64(1), count)
	assert.True(t, alertTriggered)

	// Test ScanImageFunc error
	ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
		return nil, errors.New("scan error")
	}
	_, err = ExecuteAndSaveScan(context.Background(), nil, imageName)
	assert.Error(t, err)

	// Test DB error during save
	ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
		return map[string]interface{}{"result": "ok"}, nil
	}
	db.GormDB.Migrator().DropTable(&db.ImageScanResult{})
	_, err = ExecuteAndSaveScan(context.Background(), nil, imageName)
	assert.NoError(t, err) // Returns nil even if DB save fails
}
