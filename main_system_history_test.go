package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestHandleGETSystemHistory_ExactRowsAndNodeIsolation(t *testing.T) {
	require.NoError(t, db.InitDB(":memory:"))
	originalMode, originalNode := LighthouseMode, NodeID
	LighthouseMode, NodeID = "hub", "history-hub"
	t.Cleanup(func() { LighthouseMode, NodeID = originalMode, originalNode })
	now := time.Now().UTC().Truncate(time.Second)
	samples := []db.SystemStat{
		{NodeID: "", CPU: 10, Memory: 100, Timestamp: now.Add(-3 * time.Hour)},
		{NodeID: "history-hub", CPU: 20, Memory: 200, Timestamp: now.Add(-2 * time.Hour)},
		{NodeID: "edge-one", CPU: 37.5, Memory: 1024, NetRxBytes: 11, NetTxBytes: 13, DiskReadBytes: 17, DiskWriteBytes: 19, Timestamp: now.Add(-30 * time.Minute)},
		{NodeID: "edge-two", CPU: 99, Memory: 990, Timestamp: now.Add(-time.Hour)},
		{NodeID: "edge-one", CPU: 88, Memory: 880, Timestamp: now.AddDate(0, 0, -40)},
	}
	require.NoError(t, db.GormDB.Create(&samples).Error)
	tests := []struct {
		name  string
		query url.Values
		want  []int
	}{
		{name: "default hub includes only local and legacy rows", want: []int{1, 0}},
		{name: "explicit hub", query: url.Values{"node_id": {"history-hub"}}, want: []int{1, 0}},
		{name: "first spoke", query: url.Values{"node_id": {"edge-one"}}, want: []int{2}},
		{name: "second spoke", query: url.Values{"node_id": {"edge-two"}}, want: []int{3}},
		{name: "unknown node is empty", query: url.Values{"node_id": {"missing"}}, want: []int{}},
		{name: "hour filter", query: url.Values{"node_id": {"edge-one"}, "duration": {"1h"}}, want: []int{2}},
		{name: "invalid duration uses default window", query: url.Values{"node_id": {"edge-one"}, "duration": {"abch"}}, want: []int{2}},
		{name: "zero duration uses default window", query: url.Values{"node_id": {"edge-one"}, "duration": {"0h"}}, want: []int{2}},
		{name: "negative duration uses default window", query: url.Values{"node_id": {"edge-one"}, "duration": {"-1h"}}, want: []int{2}},
		{name: "RFC3339 range", query: url.Values{"node_id": {"edge-one"}, "from": {now.Add(-time.Hour).Format(time.RFC3339)}, "to": {now.Format(time.RFC3339)}}, want: []int{2}},
		{name: "range excludes recent sample", query: url.Values{"node_id": {"edge-one"}, "from": {now.Add(-4 * time.Hour).Format(time.RFC3339)}, "to": {now.Add(-2 * time.Hour).Format(time.RFC3339)}}, want: []int{}},
		{name: "longer day window", query: url.Values{"node_id": {"edge-one"}, "days": {"60"}}, want: []int{2, 4}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			echoServer := echo.New()
			request := httptest.NewRequest(http.MethodGet, "/api/system/history?"+test.query.Encode(), nil)
			response := httptest.NewRecorder()
			require.NoError(t, handleGETSystemHistory()(echoServer.NewContext(request, response)))
			require.Equal(t, http.StatusOK, response.Code)
			var rows []map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &rows))
			require.Len(t, rows, len(test.want))
			if len(test.want) == 0 {
				assert.JSONEq(t, `[]`, response.Body.String())
			}
			for index, sampleIndex := range test.want {
				want := samples[sampleIndex]
				row := rows[index]
				assert.Equal(t, want.NodeID, row["node_id"])
				assert.Equal(t, want.CPU, row["cpu"])
				assert.Equal(t, float64(want.Memory), row["memory"])
				assert.Equal(t, float64(want.NetRxBytes), row["net_rx"])
				assert.Equal(t, float64(want.NetTxBytes), row["net_tx"])
				assert.Equal(t, float64(want.DiskReadBytes), row["disk_read"])
				assert.Equal(t, float64(want.DiskWriteBytes), row["disk_write"])
				timestamp, err := time.Parse(time.RFC3339Nano, row["timestamp"].(string))
				require.NoError(t, err)
				assert.True(t, want.Timestamp.Equal(timestamp))
			}
		})
	}
}

func TestHandleGETContainersIdHistory_ExactValues(t *testing.T) {
	require.NoError(t, db.InitDB(":memory:"))
	originalMode, originalNode := LighthouseMode, NodeID
	LighthouseMode, NodeID = "hub", "history-hub"
	t.Cleanup(func() { LighthouseMode, NodeID = originalMode, originalNode })
	now := time.Now().UTC().Truncate(time.Second)
	samples := []db.Stat{
		{ContainerID: "longid123", NodeID: "history-hub", CPU: 12.5, Memory: 1024, NetRxBytes: 11, NetTxBytes: 13, DiskReadBytes: 17, DiskWriteBytes: 19, Timestamp: now.Add(-30 * time.Minute)},
		{ContainerID: "longid123", NodeID: "", CPU: 21, Memory: 210, Timestamp: now.Add(-2 * time.Hour)},
		{ContainerID: "longid123", NodeID: "history-hub", CPU: 22, Memory: 220, Timestamp: now.AddDate(0, 0, -40)},
		{ContainerID: "longid123", NodeID: "edge-one", CPU: 99, Memory: 990, Timestamp: now.Add(-10 * time.Minute)},
		{ContainerID: "unrelated", NodeID: "history-hub", CPU: 88, Memory: 880, Timestamp: now.Add(-time.Minute)},
	}
	require.NoError(t, db.GormDB.Create(&samples).Error)
	cli := mockDockerClientWithRoundTripper(t, inspectHandler("web-local", "alpine", true))
	tests := []struct {
		name  string
		query url.Values
		want  []int
	}{
		{name: "default excludes other nodes and containers", want: []int{0, 1, 2}},
		{name: "duration uses local values", query: url.Values{"duration": {"24h"}}, want: []int{0, 1}},
		{name: "RFC3339 range", query: url.Values{"from": {now.Add(-time.Hour).Format(time.RFC3339)}, "to": {now.Format(time.RFC3339)}}, want: []int{0}},
		{name: "empty range", query: url.Values{"from": {now.Add(time.Hour).Format(time.RFC3339)}, "to": {now.Add(2 * time.Hour).Format(time.RFC3339)}}, want: []int{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			echoServer := echo.New()
			request := httptest.NewRequest(http.MethodGet, "/api/containers/c1/history?"+test.query.Encode(), nil)
			response := httptest.NewRecorder()
			requestContext := echoServer.NewContext(request, response)
			requestContext.SetParamNames("id")
			requestContext.SetParamValues("c1")
			mockUserContext(requestContext, 1, true)
			require.NoError(t, handleGETContainersIdHistory(cli)(requestContext))
			require.Equal(t, http.StatusOK, response.Code)
			var rows []map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &rows))
			require.Len(t, rows, len(test.want))
			if len(test.want) == 0 {
				assert.JSONEq(t, `[]`, response.Body.String())
			}
			for index, sampleIndex := range test.want {
				want, row := samples[sampleIndex], rows[index]
				assert.Equal(t, want.CPU, row["cpu"])
				assert.Equal(t, float64(want.Memory), row["memory"])
				assert.Equal(t, float64(want.NetRxBytes), row["net_rx"])
				assert.Equal(t, float64(want.NetTxBytes), row["net_tx"])
				assert.Equal(t, float64(want.DiskReadBytes), row["disk_read"])
				assert.Equal(t, float64(want.DiskWriteBytes), row["disk_write"])
			}
		})
	}
	t.Run("database failure is not an empty success", func(t *testing.T) {
		require.NoError(t, db.GormDB.Migrator().DropTable(&db.Stat{}))
		requestContext, response := newContainerScopedRequest(http.MethodGet, "/api/containers/c1/history", "c1")
		mockUserContext(requestContext, 1, true)
		require.NoError(t, handleGETContainersIdHistory(cli)(requestContext))
		assert.Equal(t, http.StatusInternalServerError, response.Code)
	})
}
