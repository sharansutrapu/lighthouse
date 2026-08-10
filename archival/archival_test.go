package archival

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
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
		ContainerID: "test_container",
		Timestamp:   now.Add(-5 * time.Minute),
		CPU:         50.0,
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

// --- Merged from extra_test.go ---

func TestReloadSchedule_CronJobExecution(t *testing.T) {
	db.InitDB(":memory:")
	db.GormDB.Exec("CREATE TABLE IF NOT EXISTS settings (id INTEGER PRIMARY KEY, archival_enabled BOOLEAN, archival_cron TEXT, archival_provider TEXT, archive_metrics BOOLEAN, archive_logs BOOLEAN)")
	s := db.Setting{
		ID:               1,
		ArchivalEnabled:  true,
		ArchivalCron:     "* * * * * *",
		ArchivalProvider: "unknown",
	}
	db.GormDB.Save(&s)

	InitScheduler()

	time.Sleep(1200 * time.Millisecond) // Let cron job run and fail

	// Let's test success path (mock docker to avoid real docker calls)
	os.Setenv("TEST_MOCK_DOCKER", "1")
	defer os.Unsetenv("TEST_MOCK_DOCKER")
	s.ArchivalProvider = "s3"
	s.ArchivalEndpoint = "s3.amazonaws.com"
	s.ArchiveMetrics = false
	s.ArchiveLogs = false
	db.GormDB.Save(&s)

	os.Setenv("TEST_UPLOAD_SKIP", "1")
	defer os.Unsetenv("TEST_UPLOAD_SKIP")

	// Add some stats to cover loops
	db.GormDB.AutoMigrate(&db.Stat{}, &db.SystemStat{})
	db.GormDB.Create(&db.Stat{Timestamp: time.Now()})
	db.GormDB.Create(&db.SystemStat{Timestamp: time.Now()})

	// wait for it to execute
	time.Sleep(2 * time.Second)

	ReloadSchedule()
	time.Sleep(2 * time.Second)
}

func TestReloadSchedule_InvalidCron(t *testing.T) {
	db.InitDB(":memory:")
	db.GormDB.Exec("CREATE TABLE IF NOT EXISTS settings (id INTEGER PRIMARY KEY, archival_enabled BOOLEAN, archival_cron TEXT, archival_provider TEXT, archive_metrics BOOLEAN, archive_logs BOOLEAN)")
	s := db.Setting{
		ID:               1,
		ArchivalEnabled:  true,
		ArchivalCron:     "invalid cron",
		ArchivalProvider: "unknown",
	}
	db.GormDB.Save(&s)

	InitScheduler()
	ReloadSchedule()
}

func TestArchiveLogs_SuccessWithMockServer(t *testing.T) {
	os.Setenv("TEST_MOCK_CONTAINERLIST", "1")
	defer os.Unsetenv("TEST_MOCK_CONTAINERLIST")

	provider := &mockProvider{}
	_ = archiveLogs(context.Background(), provider, "bucket", "date", "timestamp", time.Now(), time.Now())
}

func TestArchiveMetrics_Direct(t *testing.T) {
	db.InitDB(":memory:")
	db.GormDB.AutoMigrate(&db.Stat{}, &db.SystemStat{})
	db.GormDB.Create(&db.Stat{Timestamp: time.Now()})
	db.GormDB.Create(&db.SystemStat{Timestamp: time.Now()})

	provider := &mockProvider{}
	os.Setenv("TEST_NO_STATS_ERR", "1")
	err := archiveMetrics(context.Background(), provider, "bucket", "date", "timestamp", time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour))
	assert.Error(t, err)
	os.Unsetenv("TEST_NO_STATS_ERR")

	os.Setenv("TEST_NO_STATS_ERR2", "1")
	err = archiveMetrics(context.Background(), provider, "bucket", "date", "timestamp", time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour))
	assert.Error(t, err)
	os.Unsetenv("TEST_NO_STATS_ERR2")

	err = archiveMetrics(context.Background(), provider, "bucket", "date", "timestamp", time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour))
	assert.NoError(t, err)
}

func TestReloadSchedule_DBFirstError(t *testing.T) {
	db.InitDB(":memory:")
	db.GormDB.Exec("DELETE FROM settings")
	InitScheduler()
	ReloadSchedule()
}

func TestRunArchival_UnknownProvider(t *testing.T) {
	s := db.Setting{ArchivalProvider: "unknown"}
	err := RunArchival(s)
	assert.Error(t, err)
}

func TestRunArchival_GCS(t *testing.T) {
	s := db.Setting{
		ArchivalProvider: "gcs",
		ArchivalAuth1:    "{\"type\": \"service_account\"}", // invalid creds for actual connect, might error
	}
	_ = RunArchival(s)
}

func TestRunArchival_Azure(t *testing.T) {
	s := db.Setting{
		ArchivalProvider: "azure",
		ArchivalAuth1:    "account",
		ArchivalAuth2:    "invalidbase64!@#", // errors on NewAzureProvider
	}
	err := RunArchival(s)
	assert.Error(t, err)

	s.ArchivalAuth2 = "a2V5"
	s.ArchiveMetrics = true
	os.Setenv("TEST_UPLOAD_SKIP", "1")
	defer os.Unsetenv("TEST_UPLOAD_SKIP")
	_ = RunArchival(s)
}

func TestRunArchival_S3(t *testing.T) {
	s := db.Setting{
		ArchivalEnabled:  true,
		ArchivalCron:     "* * * * * *",
		ArchivalProvider: "s3",
		ArchivalEndpoint: "http://localhost",
		ArchiveMetrics:   true,
		ArchiveLogs:      true,
	}
	os.Setenv("TEST_UPLOAD_SKIP", "1")
	defer os.Unsetenv("TEST_UPLOAD_SKIP")
	_ = RunArchival(s)
}

func TestArchiveMetrics_OsCreateErr(t *testing.T) {
	oldTmp := os.Getenv("TMPDIR")
	os.Setenv("TMPDIR", "/nonexistent/path/dir")
	defer os.Setenv("TMPDIR", oldTmp)

	provider := &mockProvider{}
	err := archiveMetrics(context.Background(), provider, "bucket", "date", "timestamp", time.Now(), time.Now())
	assert.Error(t, err)
}

func TestArchiveLogs_OsCreateErr(t *testing.T) {
	oldTmp := os.Getenv("TMPDIR")
	os.Setenv("TMPDIR", "/nonexistent/path/dir")
	defer os.Setenv("TMPDIR", oldTmp)

	mockHTTPClient = &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`[]`)),
				}, nil
			},
		},
	}
	defer func() { mockHTTPClient = nil }()

	provider := &mockProvider{}
	err := archiveLogs(context.Background(), provider, "bucket", "date", "timestamp", time.Now(), time.Now())
	assert.Error(t, err)
}

func TestArchiveLogs_NewClientErr(t *testing.T) {
	os.Setenv("TEST_MOCK_NEW_CLIENT_ERR", "1")
	defer os.Unsetenv("TEST_MOCK_NEW_CLIENT_ERR")

	provider := &mockProvider{}
	err := archiveLogs(context.Background(), provider, "bucket", "date", "timestamp", time.Now(), time.Now())
	assert.Error(t, err)
}

func TestArchiveLogs_CliErr(t *testing.T) {
	os.Setenv("TEST_MOCK_CLIER", "1")
	defer os.Unsetenv("TEST_MOCK_CLIER")

	mockHTTPClient = &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`[]`)),
				}, nil
			},
		},
	}
	defer func() { mockHTTPClient = nil }()

	provider := &mockProvider{}
	err := archiveLogs(context.Background(), provider, "bucket", "date", "timestamp", time.Now(), time.Now())
	assert.Error(t, err)
}

func TestArchiveLogs_TwErr(t *testing.T) {
	os.Setenv("TEST_MOCK_TW_WRITE_ERR", "1")
	defer os.Unsetenv("TEST_MOCK_TW_WRITE_ERR")

	testTwErrs(t)
}

func TestArchiveLogs_TwHeaderErr(t *testing.T) {
	os.Setenv("TEST_MOCK_TW_HEADER_ERR", "1")
	defer os.Unsetenv("TEST_MOCK_TW_HEADER_ERR")

	testTwErrs(t)
}

func testTwErrs(t *testing.T) {
	mockHTTPClient = &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/_ping" {
					return &http.Response{
						StatusCode: 200,
						Header:     make(http.Header),
						Body:       io.NopCloser(bytes.NewBufferString("OK")),
					}, nil
				}
				if strings.HasSuffix(req.URL.Path, "/containers/json") {
					return &http.Response{
						StatusCode: 200,
						Header:     make(http.Header),
						Body:       io.NopCloser(bytes.NewBufferString(`[{"Id":"123456789012345","Names":["/test_container"]}]`)),
					}, nil
				}
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString("log data")),
				}, nil
			},
		},
	}
	defer func() { mockHTTPClient = nil }()

	os.Setenv("DOCKER_HOST", "tcp://127.0.0.1:12345")
	defer os.Unsetenv("DOCKER_HOST")

	provider := &mockProvider{}
	_ = archiveLogs(context.Background(), provider, "bucket", "date", "timestamp", time.Now().Add(-1*time.Hour), time.Now())
}

func TestExtractContainers_Branches(t *testing.T) {
	// m.([]interface{}) branch
	input1 := []interface{}{
		map[string]interface{}{"Id": "123"},
		"not-a-map",
	}
	res1 := extractContainers(input1)
	assert.Len(t, res1, 1)

	// map branch with slice inside
	input2 := map[string]interface{}{
		"containers": []interface{}{
			map[string]interface{}{"Id": "456"},
		},
	}
	res2 := extractContainers(input2)
	assert.Len(t, res2, 1)

	// other branch
	input3 := "not-a-map-or-slice"
	res3 := extractContainers(input3)
	assert.Nil(t, res3)
}

type mockTransport struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTrip(req)
}

func TestArchiveLogs_MockDocker(t *testing.T) {
	mockHTTPClient = &http.Client{
		Transport: &mockTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/_ping" {
					return &http.Response{
						StatusCode: 200,
						Header:     make(http.Header),
						Body:       io.NopCloser(bytes.NewBufferString("OK")),
					}, nil
				}
				if strings.HasSuffix(req.URL.Path, "/containers/json") {
					return &http.Response{
						StatusCode: 200,
						Header:     make(http.Header),
						Body: io.NopCloser(bytes.NewBufferString(`[
							{"Id":"123456789012345","Names":["/test_container"]},
							{"Id":"abc"},
							{"Id":"def","Names":[]},
							{"Id":"error_container"},
							{"Id":"empty_container"}
						]`)),
					}, nil
				}
				if strings.Contains(req.URL.Path, "error_container/logs") {
					return &http.Response{
						StatusCode: 500,
						Body:       io.NopCloser(bytes.NewBufferString("error")),
					}, nil
				}
				if strings.Contains(req.URL.Path, "empty_container/logs") {
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewBufferString("")),
					}, nil
				}
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString("log data")),
				}, nil
			},
		},
	}
	defer func() { mockHTTPClient = nil }()

	// Provide a dummy docker host so it doesn't fail looking for a socket
	os.Setenv("DOCKER_HOST", "tcp://127.0.0.1:12345")
	defer os.Unsetenv("DOCKER_HOST")

	provider := &mockProvider{}
	err := archiveLogs(context.Background(), provider, "bucket", "date", "timestamp", time.Now().Add(-1*time.Hour), time.Now())
	assert.NoError(t, err)
}
