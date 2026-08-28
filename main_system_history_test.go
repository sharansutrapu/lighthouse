package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

// TestHandleGETSystemHistory_Table covers every query-filter branch: from/to
// range, duration suffix, default day window, and DB query failure.
func TestHandleGETSystemHistory_Table(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		dropTable  bool
		wantStatus int
	}{
		{name: "happy path: default 30-day window", query: "", wantStatus: http.StatusOK},
		{name: "happy path: duration filter", query: "?duration=6h", wantStatus: http.StatusOK},
		{name: "hostile: malformed duration falls back to default window", query: "?duration=abch", wantStatus: http.StatusOK},
		{name: "happy path: from/to range", query: "?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z", wantStatus: http.StatusOK},
		{name: "happy path: custom days param", query: "?days=7", wantStatus: http.StatusOK},
		{name: "hostile: non-numeric days falls back to 30", query: "?days=notanumber", wantStatus: http.StatusOK},
		{name: "infra failure: DB query fails", dropTable: true, wantStatus: http.StatusInternalServerError},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			db.GormDB.Create(&db.SystemStat{CPU: 1.1, Memory: 100, Timestamp: time.Now()})
			if tc.dropTable {
				db.GormDB.Migrator().DropTable(&db.SystemStat{})
			}
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/system/history"+tc.query, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			h := handleGETSystemHistory()
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}
