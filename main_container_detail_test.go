package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/cluster"
	"lighthouse/db"
)

// dockerLogFrame builds one Docker multiplexed log frame (used when the
// container's Config.Tty is false): [streamType, 0,0,0, size(4 bytes BE)] + payload.
func dockerLogFrame(streamType byte, payload string) []byte {
	n := len(payload)
	header := []byte{streamType, 0, 0, 0, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	return append(header, []byte(payload)...)
}

func newContainerScopedRequest(method, path, id string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)
	return c, rec
}

func inspectHandler(name, image string, tty bool) func(req *http.Request) (*http.Response, error) {
	ttyStr := "false"
	if tty {
		ttyStr = "true"
	}
	return func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/json") && !strings.Contains(req.URL.Path, "/logs") {
			return makeResponse(http.StatusOK, `{"Id":"longid123","Name":"`+name+`","Config":{"Image":"`+image+`","Tty":`+ttyStr+`}}`), nil
		}
		return makeResponse(http.StatusOK, `{}`), nil
	}
}

// ─── handleGETContainersIdInspect ──────────────────────────────────────────

func TestHandleGETContainersIdInspect_Table(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		isAdmin    bool
		userID     int
		seedUser   *db.User
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{name: "hostile: invalid id", id: "../etc", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", false), wantStatus: http.StatusBadRequest},
		{name: "infra failure: inspect error", id: "c1", isAdmin: true, userID: 1, handler: func(req *http.Request) (*http.Response, error) { return nil, assertErr("boom") }, wantStatus: http.StatusNotFound},
		{
			name: "hostile: non-admin unauthorized", id: "c1", isAdmin: false, userID: 50,
			seedUser: &db.User{ID: 50, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"},
			handler:  inspectHandler("target", "alpine", false), wantStatus: http.StatusForbidden,
		},
		{
			name: "hostile: excluded container", id: "c1", isAdmin: true, userID: 1,
			handler:    inspectHandler("secret-internal", "alpine", false),
			wantStatus: http.StatusNotFound,
		},
		{name: "happy path: admin sees full inspect", id: "c1", isAdmin: true, userID: 1, handler: inspectHandler("target", "alpine", false), wantStatus: http.StatusOK},
		{
			name: "happy path: authorized non-admin", id: "c1", isAdmin: false, userID: 51,
			seedUser: &db.User{ID: 51, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "target"},
			handler:  inspectHandler("target", "alpine", false), wantStatus: http.StatusOK,
		},
	}

	origExcluded := excludedContainerNames
	excludedContainerNames = []string{"secret-internal"}
	t.Cleanup(func() { excludedContainerNames = origExcluded })

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			if tc.isAdmin {
				// handleGETContainersIdInspect re-checks admin status from
				// the DB (not the JWT claim) before deciding whether to run
				// the regex authorization check.
				userID := tc.userID
				if userID == 0 {
					userID = 1
				}
				tc.userID = userID
				db.GormDB.Save(&db.User{ID: uint(userID), IsAdmin: true, IsActive: true})
			}
			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			c, rec := newContainerScopedRequest(http.MethodGet, "/containers/"+tc.id+"/inspect", tc.id)
			mockUserContext(c, uint(tc.userID), tc.isAdmin)
			h := handleGETContainersIdInspect(cli)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

// ─── handleGETContainersIdLogsDownload ─────────────────────────────────────

func TestHandleGETContainersIdLogsDownload_Table(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		isAdmin    bool
		userID     int
		seedUser   *db.User
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{name: "hostile: invalid id", id: "../x", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusBadRequest},
		{name: "infra failure: inspect error", id: "c1", isAdmin: true, userID: 1, handler: func(req *http.Request) (*http.Response, error) { return nil, assertErr("x") }, wantStatus: http.StatusNotFound},
		{
			name: "hostile: unauthorized non-admin", id: "c1", isAdmin: false, userID: 52,
			seedUser: &db.User{ID: 52, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"},
			handler:  inspectHandler("target", "alpine", true), wantStatus: http.StatusForbidden,
		},
		{
			name: "infra failure: ContainerLogs fails", id: "c1", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return nil, assertErr("log fetch failed")
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "happy path: admin downloads logs", id: "c1", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, "line one\nline two\n"), nil
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			c, rec := newContainerScopedRequest(http.MethodGet, "/containers/"+tc.id+"/logs/download", tc.id)
			mockUserContext(c, uint(tc.userID), tc.isAdmin)
			h := handleGETContainersIdLogsDownload(cli)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

// ─── handleGETContainersIdLogs ──────────────────────────────────────────────

func TestHandleGETContainersIdLogs_Table(t *testing.T) {
	tty := dockerLogFrame // shorthand
	_ = tty
	multiplexedBody := string(dockerLogFrame(1, "2024-01-01T00:00:00.000000000Z hello\n"))

	tests := []struct {
		name       string
		id         string
		query      string
		isAdmin    bool
		userID     int
		seedUser   *db.User
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{name: "hostile: invalid id", id: "../x", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusBadRequest},
		{name: "infra failure: inspect error", id: "c1", isAdmin: true, userID: 1, handler: func(req *http.Request) (*http.Response, error) { return nil, assertErr("x") }, wantStatus: http.StatusNotFound},
		{
			name: "hostile: unauthorized non-admin", id: "c1", isAdmin: false, userID: 53,
			seedUser: &db.User{ID: 53, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"},
			handler:  inspectHandler("target", "alpine", true), wantStatus: http.StatusForbidden,
		},
		{
			name: "infra failure: ContainerLogs fails", id: "c1", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return nil, assertErr("boom")
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "happy path: TTY container, no until, initial load", id: "c1", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, "2024-01-01T00:00:00.000000000Z hello\n"), nil
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "happy path: non-TTY container demuxed, no until", id: "c1", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, multiplexedBody), nil
				}
				return inspectHandler("target", "alpine", false)(req)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "happy path: until=RFC3339Nano filters correctly", id: "c1", query: "?until=2024-01-01T00:00:00.000000000Z", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, "2024-01-01T00:00:00.000000000Z hello\n"), nil
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "happy path: until=RFC3339 (no nanos)", id: "c1", query: "?until=2024-01-01T00:00:00Z", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, "2024-01-01T00:00:00.000000000Z hello\n"), nil
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "happy path: until=unix timestamp", id: "c1", query: "?until=1704067200", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, "2024-01-01T00:00:00.000000000Z hello\n"), nil
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "hostile: until is garbage -> 400", id: "c1", query: "?until=not-a-timestamp", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, "2024-01-01T00:00:00.000000000Z hello\n"), nil
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/containers/"+tc.id+"/logs"+tc.query, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockUserContext(c, uint(tc.userID), tc.isAdmin)
			h := handleGETContainersIdLogs(cli)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

// ─── handleGETContainersIdLogsCount ─────────────────────────────────────────

func TestHandleGETContainersIdLogsCount_Table(t *testing.T) {
	multiplexedBody := string(dockerLogFrame(1, "line1\n")) + string(dockerLogFrame(1, "line2\n"))

	tests := []struct {
		name       string
		id         string
		isAdmin    bool
		userID     int
		seedUser   *db.User
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{name: "hostile: invalid id", id: "../x", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusBadRequest},
		{name: "infra failure: inspect error", id: "c1", isAdmin: true, userID: 1, handler: func(req *http.Request) (*http.Response, error) { return nil, assertErr("x") }, wantStatus: http.StatusNotFound},
		{
			name: "hostile: unauthorized non-admin", id: "c1", isAdmin: false, userID: 54,
			seedUser: &db.User{ID: 54, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"},
			handler:  inspectHandler("target", "alpine", true), wantStatus: http.StatusForbidden,
		},
		{
			name: "infra failure: ContainerLogs fails", id: "c1", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return nil, assertErr("boom")
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "happy path: TTY count", id: "c1", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, "a\nb\nc\n"), nil
				}
				return inspectHandler("target", "alpine", true)(req)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "happy path: non-TTY demuxed count", id: "c1", isAdmin: true, userID: 1,
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/logs") {
					return makeResponse(http.StatusOK, multiplexedBody), nil
				}
				return inspectHandler("target", "alpine", false)(req)
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			c, rec := newContainerScopedRequest(http.MethodGet, "/containers/"+tc.id+"/logs/count", tc.id)
			mockUserContext(c, uint(tc.userID), tc.isAdmin)
			h := handleGETContainersIdLogsCount(cli)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

// ─── handleGETContainersIdStatsNow ──────────────────────────────────────────

func TestHandleGETContainersIdStatsNow_Table(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		isAdmin    bool
		userID     int
		seedUser   *db.User
		seedCache  bool
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{name: "hostile: invalid id", id: "../x", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusBadRequest},
		{name: "infra failure: inspect error", id: "c1", isAdmin: true, userID: 1, handler: func(req *http.Request) (*http.Response, error) { return nil, assertErr("x") }, wantStatus: http.StatusNotFound},
		{
			name: "hostile: unauthorized non-admin", id: "statsnow-c1", isAdmin: false, userID: 55,
			seedUser: &db.User{ID: 55, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"},
			handler:  inspectHandler("target", "alpine", true), wantStatus: http.StatusForbidden,
		},
		{name: "happy path: cache miss returns zeros", id: "statsnow-miss", isAdmin: true, userID: 1, handler: inspectHandler("target", "alpine", true), wantStatus: http.StatusOK},
		{name: "happy path: cache hit returns live values", id: "statsnow-hit", isAdmin: true, userID: 1, seedCache: true, handler: inspectHandler("target", "alpine", true), wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			if tc.seedCache {
				liveStatsMu.Lock()
				liveStatsCache[tc.id] = struct {
					CPU            float64
					Memory         int64
					NetRxBytes     int64
					NetTxBytes     int64
					DiskReadBytes  int64
					DiskWriteBytes int64
				}{CPU: 12.5, Memory: 1024, NetRxBytes: 10, NetTxBytes: 20, DiskReadBytes: 30, DiskWriteBytes: 40}
				liveStatsMu.Unlock()
				t.Cleanup(func() {
					liveStatsMu.Lock()
					delete(liveStatsCache, tc.id)
					liveStatsMu.Unlock()
				})
			}
			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			c, rec := newContainerScopedRequest(http.MethodGet, "/containers/"+tc.id+"/stats-now", tc.id)
			mockUserContext(c, uint(tc.userID), tc.isAdmin)
			h := handleGETContainersIdStatsNow(cli)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

// ─── handleGETContainersIdHistory ───────────────────────────────────────────

func TestHandleGETContainersIdHistory_Table(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		query      string
		isAdmin    bool
		userID     int
		seedUser   *db.User
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{name: "hostile: invalid id", id: "../x", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusBadRequest},
		{name: "infra failure: inspect error", id: "c1", isAdmin: true, userID: 1, handler: func(req *http.Request) (*http.Response, error) { return nil, assertErr("x") }, wantStatus: http.StatusNotFound},
		{
			name: "hostile: unauthorized non-admin", id: "c1", isAdmin: false, userID: 56,
			seedUser: &db.User{ID: 56, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"},
			handler:  inspectHandler("target", "alpine", true), wantStatus: http.StatusForbidden,
		},
		{name: "happy path: default window (no filters)", id: "c1", isAdmin: true, userID: 1, handler: inspectHandler("target", "alpine", true), wantStatus: http.StatusOK},
		{name: "happy path: duration filter", id: "c1", query: "?duration=24h", isAdmin: true, userID: 1, handler: inspectHandler("target", "alpine", true), wantStatus: http.StatusOK},
		{name: "happy path: from/to range filter", id: "c1", query: "?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z", isAdmin: true, userID: 1, handler: inspectHandler("target", "alpine", true), wantStatus: http.StatusOK},
		{name: "hostile: malformed duration is ignored gracefully", id: "c1", query: "?duration=notanumberh", isAdmin: true, userID: 1, handler: inspectHandler("target", "alpine", true), wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			db.GormDB.Create(&db.Stat{ContainerID: "longid123", CPU: 1.1, Memory: 100, Timestamp: time.Now()})
			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/containers/"+tc.id+"/history"+tc.query, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockUserContext(c, uint(tc.userID), tc.isAdmin)
			h := handleGETContainersIdHistory(cli)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

// ─── handlePOSTContainersIdScan ─────────────────────────────────────────────

func TestHandlePOSTContainersIdScan_Table(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		image      string
		isAdmin    bool
		canScan    bool
		userID     int
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{name: "hostile: forbidden, no scan permission", id: "c1", isAdmin: false, canScan: false, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusForbidden},
		{name: "hostile: invalid id", id: "../x", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusBadRequest},
		{name: "hostile: invalid image query param", id: "c1", image: "--server=http://evil", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusBadRequest},
		{name: "happy path: admin triggers scan (no image param)", id: "c1", isAdmin: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusOK},
		{name: "happy path: authorized non-admin (CanRunScans) triggers scan with explicit image", id: "c1", image: "alpine:3.19", isAdmin: false, canScan: true, userID: 1, handler: inspectHandler("c1", "alpine", true), wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			e := echo.New()
			target := "/containers/" + tc.id + "/scan"
			if tc.image != "" {
				target += "?image=" + tc.image
			}
			req := httptest.NewRequest(http.MethodPost, target, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			claims := &UserClaims{ID: tc.userID, IsAdmin: tc.isAdmin, CanRunScans: tc.canScan}
			tok := jwt.New(jwt.SigningMethodHS256)
			tok.Claims = claims
			c.Set("user", tok)
			h := handlePOSTContainersIdScan(cli)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

// TestHandlePOSTContainersIdScan_HubModeSpokeDispatch covers the hub-mode
// branch that dispatches a scan to a Spoke node instead of scanning locally.
// SendCommandToSpoke's return error is intentionally ignored by the handler,
// so this always reports 200 even though no spoke is actually connected.
func TestHandlePOSTContainersIdScan_HubModeSpokeDispatch(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	origMode := LighthouseMode
	LighthouseMode = "hub"
	t.Cleanup(func() { LighthouseMode = origMode })

	cluster.GlobalHub.Lock()
	cluster.GlobalHub.SpokeContainers["node1"] = []map[string]interface{}{{"Id": "spoke-c1"}}
	cluster.GlobalHub.Unlock()
	t.Cleanup(func() {
		cluster.GlobalHub.Lock()
		delete(cluster.GlobalHub.SpokeContainers, "node1")
		cluster.GlobalHub.Unlock()
	})

	cli := mockDockerClientWithRoundTripper(t, inspectHandler("spoke-c1", "alpine", false))
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/containers/spoke-c1/scan", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("spoke-c1")
	claims := &UserClaims{ID: 1, IsAdmin: true}
	tok := jwt.New(jwt.SigningMethodHS256)
	tok.Claims = claims
	c.Set("user", tok)

	h := handlePOSTContainersIdScan(cli)
	assert.NoError(t, h(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Spoke node")
}

// ─── handleGETImagesScans ────────────────────────────────────────────────

func TestHandleGETImagesScans_Table(t *testing.T) {
	tests := []struct {
		name       string
		image      string
		seed       bool
		wantStatus int
	}{
		{name: "hostile: missing image param", image: "", wantStatus: http.StatusBadRequest},
		{name: "infra: no scan result found", image: "never-scanned", wantStatus: http.StatusNotFound},
		{name: "happy path: scan result found", image: "alpine", seed: true, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seed {
				db.GormDB.Create(&db.ImageScanResult{Image: "alpine", Result: "{}"})
			}
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/images/scans?image="+tc.image, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			h := handleGETImagesScans()
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}
