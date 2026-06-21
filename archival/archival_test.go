package archival

import (
	"context"
	"testing"
	"time"

	"lighthouse/db"

	"github.com/stretchr/testify/assert"
)

// mockProvider implements backup.StorageProvider for testing
type mockProvider struct {
	UploadCalled bool
	Bucket       string
	ObjectName   string
}

func (m *mockProvider) Upload(ctx context.Context, bucket, objectName, filePath string) error {
	m.UploadCalled = true
	m.Bucket = bucket
	m.ObjectName = objectName
	return nil
}

func TestExtractContainers(t *testing.T) {
	// Test basic extraction
	input := []interface{}{
		map[string]interface{}{"Id": "12345", "Names": []interface{}{"/test"}},
	}

	containers := extractContainers(input)
	assert.Len(t, containers, 1)
	assert.Equal(t, "12345", containers[0]["Id"])
}

func TestArchiveMetrics(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)

	// Insert test data
	now := time.Now()
	err = db.GormDB.Create(&db.Stat{
		ContainerID:   "test_container",
		Timestamp:     now.Add(-5 * time.Minute),
		CPU:           50.0,
	}).Error
	assert.NoError(t, err)

	provider := &mockProvider{}
	ctx := context.Background()

	// Should create a gzip file and upload it
	err = archiveMetrics(ctx, provider, "test-bucket", "2026-01-01", "2026-01-01_00-00-00", now.Add(-10*time.Minute), now)
	assert.NoError(t, err)

	assert.True(t, provider.UploadCalled)
	assert.Equal(t, "test-bucket", provider.Bucket)
	assert.Contains(t, provider.ObjectName, "metrics_")
}
