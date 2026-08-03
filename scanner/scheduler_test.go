package scanner

import (
	"context"
	"testing"

	"lighthouse/db"

	"github.com/glebarez/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) {
	var err error
	db.GormDB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.GormDB.AutoMigrate(&db.Setting{}, &db.ImageScanResult{})
}

func TestReloadSchedule(t *testing.T) {
	setupTestDB(t)

	// Test 1: No schedule enabled
	db.GormDB.Create(&db.Setting{
		ID:                   1,
		ScheduledScanEnabled: false,
		ScheduledScanCron:    "0 0 * * *",
	})
	ReloadSchedule(nil)
	assert.Equal(t, 0, len(scheduler.Entries()), "Scheduler should have 0 entries when disabled")

	// Test 2: Schedule enabled
	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("scheduled_scan_enabled", true)
	ReloadSchedule(nil)
	assert.Equal(t, 1, len(scheduler.Entries()), "Scheduler should have 1 entry when enabled")
	
	entryID := currentEntryID
	
	// Test 3: Reload with new cron replaces old
	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("scheduled_scan_cron", "0 1 * * *")
	ReloadSchedule(nil)
	assert.Equal(t, 1, len(scheduler.Entries()), "Scheduler should still have 1 entry")
	assert.NotEqual(t, entryID, currentEntryID, "Scheduler entry ID should have changed")

	// Test 4: Disable removes the entry
	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("scheduled_scan_enabled", false)
	ReloadSchedule(nil)
	assert.Equal(t, 0, len(scheduler.Entries()), "Scheduler should have 0 entries when disabled again")
}

func TestExecuteAndSaveScan(t *testing.T) {
	setupTestDB(t)

	// Mock the ScanImageFunc
	originalScanImageFunc := ScanImageFunc
	defer func() { ScanImageFunc = originalScanImageFunc }()

	var alertTriggered bool
	AlertCallback = func(imageName string, resultBytes []byte) {
		alertTriggered = true
	}
	defer func() { AlertCallback = nil }()

	ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
		return map[string]interface{}{
			"Results": []map[string]interface{}{
				{
					"Vulnerabilities": []map[string]interface{}{
						{"Severity": "CRITICAL"},
					},
				},
			},
		}, nil
	}

	imageName := "test-image:latest"
	_, err := ExecuteAndSaveScan(context.Background(), nil, imageName)
	assert.NoError(t, err)

	// Assert DB was updated
	var count int64
	db.GormDB.Model(&db.ImageScanResult{}).Where("image = ?", imageName).Count(&count)
	assert.Equal(t, int64(1), count, "Expected 1 ImageScanResult in DB")

	var result db.ImageScanResult
	db.GormDB.First(&result)
	assert.Contains(t, result.Result, "CRITICAL", "Expected DB result to contain CRITICAL vulnerability")

	// Assert callback was triggered
	assert.True(t, alertTriggered, "Expected AlertCallback to be triggered")
}
