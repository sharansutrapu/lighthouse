package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func containerListRoundTripper(containersJSON string) *mockDockerRoundTripper {
	return &mockDockerRoundTripper{handler: func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/containers/json") {
			return makeResponse(http.StatusOK, containersJSON), nil
		}
		return makeResponse(http.StatusOK, `{}`), nil
	}}
}

func TestTriggerRetroactiveScans_Table(t *testing.T) {
	tests := []struct {
		name            string
		handler         func(req *http.Request) (*http.Response, error)
		seedScanResult  *db.ImageScanResult
		containersJSON  string
	}{
		{
			name:    "infra failure: ContainerList errors, returns early without panic",
			handler: func(req *http.Request) (*http.Response, error) { return nil, assertErr("docker daemon unreachable") },
		},
		{
			name:           "happy path: container with empty image is skipped",
			containersJSON: `[{"Id":"c1","Image":""}]`,
		},
		{
			name:           "happy path: image with an existing scan result is skipped",
			containersJSON: `[{"Id":"c2","Image":"already-scanned"}]`,
			seedScanResult: &db.ImageScanResult{Image: "already-scanned", Result: "{}"},
		},
		{
			name:           "happy path: image with no prior scan triggers a retroactive scan attempt",
			containersJSON: `[{"Id":"c3","Image":"never-scanned"}]`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedScanResult != nil {
				db.GormDB.Create(tc.seedScanResult)
			}
			handler := tc.handler
			if handler == nil {
				handler = containerListRoundTripper(tc.containersJSON).handler
			}
			cli := mockDockerClientWithRoundTripper(t, handler)
			assert.NotPanics(t, func() { triggerRetroactiveScans(cli) })
		})
	}
}

func TestCleanupStaleAlerts_Table(t *testing.T) {
	t.Run("happy path: no stale alerts is a no-op", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		assert.NotPanics(t, cleanupStaleAlerts)
	})

	t.Run("happy path: marks pending alerts as stale", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		db.GormDB.Create(&db.AlertHistory{RuleName: "r1", DeliveryStatus: ""})
		db.GormDB.Create(&db.AlertHistory{RuleName: "r2", DeliveryStatus: "Sent"})

		cleanupStaleAlerts()

		var pending db.AlertHistory
		assert.NoError(t, db.GormDB.Where("rule_name = ?", "r1").First(&pending).Error)
		assert.Equal(t, "Failed (Stale)", pending.DeliveryStatus)

		var sent db.AlertHistory
		assert.NoError(t, db.GormDB.Where("rule_name = ?", "r2").First(&sent).Error)
		assert.Equal(t, "Sent", sent.DeliveryStatus)
	})
}
