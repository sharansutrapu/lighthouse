package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func wsBearerHeader(token string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	return h
}

func issueTestAccessToken(t *testing.T, claims *UserClaims) string {
	t.Helper()
	initSecretKey()
	access, _, err := issueTokenPair(claims)
	assert.NoError(t, err)
	return access
}

// ---- handleGETWsSystemStats ----

func TestHandleGETWsSystemStats_PreUpgradeBranches(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true})
	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})

	t.Run("hostile: missing token", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/ws/system-stats", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		assert.NoError(t, handleGETWsSystemStats()(c))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("hostile: invalid token", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/ws/system-stats", nil)
		req.Header.Set("Authorization", "Bearer not-a-real-token")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		assert.NoError(t, handleGETWsSystemStats()(c))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("infra failure: non-websocket request fails upgrade", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/ws/system-stats", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		assert.NoError(t, handleGETWsSystemStats()(c))
	})
}

func TestHandleGETWsSystemStats_RealConnection(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true})
	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})

	sysStatsMu.Lock()
	latestSystemStats = &systemStatsSnapshot{CPU: 12.5, Memory: 1024, TotalMemory: 2048, Cores: 4, RunningContainers: 1, TotalContainers: 2}
	sysStatsMu.Unlock()
	t.Cleanup(func() {
		sysStatsMu.Lock()
		latestSystemStats = nil
		sysStatsMu.Unlock()
	})

	// Other tests (e.g. TestInitWSUpgrader) permanently mutate the shared
	// package-level upgrader.CheckOrigin; force it back to always-allow for
	// the duration of this real-connection test so the dial isn't rejected.
	origCheckOrigin := upgrader.CheckOrigin
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	t.Cleanup(func() { upgrader.CheckOrigin = origCheckOrigin })

	e := echo.New()
	e.GET("/ws/system-stats", handleGETWsSystemStats())
	srv := httptest.NewServer(e)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/system-stats"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, wsBearerHeader(token))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var payload map[string]interface{}
	assert.NoError(t, conn.ReadJSON(&payload))
	// Some other test in this process may have started a background stats
	// collector goroutine that keeps overwriting latestSystemStats; only
	// assert the shape of the payload (not exact values) to avoid flakiness.
	assert.Contains(t, payload, "cores")
	assert.Contains(t, payload, "cpu")
	assert.Contains(t, payload, "running_containers")
}

// ---- handleGETWsEvents ----

func TestHandleGETWsEvents_PreUpgradeBranches(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true})
	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})

	mockT := &mockDockerRoundTripper{handler: func(req *http.Request) (*http.Response, error) {
		return makeResponse(http.StatusOK, ""), nil
	}}
	cli, err := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	assert.NoError(t, err)

	t.Run("hostile: missing token", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		assert.NoError(t, handleGETWsEvents(cli)(c))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("infra failure: non-websocket request fails upgrade", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		assert.NoError(t, handleGETWsEvents(cli)(c))
	})
}

// dockerEventJSON builds a minimal events.Message JSON document for a
// container-named actor (no field tags on events.Message/Actor, so the
// exported Go field names are used verbatim as JSON keys).
func dockerEventJSON(containerName string) string {
	return `{"Type":"container","Action":"start","Actor":{"ID":"c1","Attributes":{"name":"` + containerName + `"}}}`
}

// eventsStreamRoundTripper serves a fake /events response whose body streams
// the given JSON documents one at a time (via io.Pipe) so the real moby
// client's decode loop delivers them to the Messages channel individually.
func eventsStreamRoundTripper(events ...string) *mockDockerRoundTripper {
	pr, pw := io.Pipe()
	go func() {
		for _, e := range events {
			pw.Write([]byte(e))
		}
		pw.Close()
	}()
	return &mockDockerRoundTripper{handler: func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/events") {
			return &http.Response{StatusCode: http.StatusOK, Body: pr, Header: make(http.Header)}, nil
		}
		return makeResponse(http.StatusOK, "[]"), nil
	}}
}

func TestHandleGETWsEvents_RealConnection_AdminSeesAllEvents(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true})
	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})

	origCheckOrigin := upgrader.CheckOrigin
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	t.Cleanup(func() { upgrader.CheckOrigin = origCheckOrigin })

	mockT := eventsStreamRoundTripper(dockerEventJSON("any-container"))
	cli, err := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	assert.NoError(t, err)

	e := echo.New()
	e.GET("/ws/events", handleGETWsEvents(cli))
	srv := httptest.NewServer(e)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, wsBearerHeader(token))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var payload map[string]interface{}
	assert.NoError(t, conn.ReadJSON(&payload))
	assert.Equal(t, "container", payload["Type"])
}

func TestHandleGETWsEvents_RealConnection_NonAdminFiltersUnauthorizedContainers(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 80, Username: "wsUser80", Email: "wsuser80@test", IsActive: true, IsRestrictedAccess: true, AllowedContainers: "^web.*$", PasswordVersion: 1, PasswordChanged: true})
	token := issueTestAccessToken(t, &UserClaims{ID: 80, IsAdmin: false, PasswordVersion: 1, PasswordChanged: true})

	origCheckOrigin := upgrader.CheckOrigin
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	t.Cleanup(func() { upgrader.CheckOrigin = origCheckOrigin })

	// First event is for an unauthorized container (should be silently
	// skipped), second is for an authorized one (should be delivered).
	mockT := eventsStreamRoundTripper(dockerEventJSON("db-server"), dockerEventJSON("web-frontend"))
	cli, err := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	assert.NoError(t, err)

	e := echo.New()
	e.GET("/ws/events", handleGETWsEvents(cli))
	srv := httptest.NewServer(e)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, wsBearerHeader(token))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var payload map[string]interface{}
	assert.NoError(t, conn.ReadJSON(&payload))
	actor, ok := payload["Actor"].(map[string]interface{})
	assert.True(t, ok)
	attrs, ok := actor["Attributes"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "web-frontend", attrs["name"])
}

// ---- handleGETWsLogsId ----

func TestHandleGETWsLogsId_PreUpgradeBranches(t *testing.T) {
	adminToken := func(t *testing.T) string {
		return issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})
	}

	tests := []struct {
		name       string
		id         string
		token      func(t *testing.T) string
		seedUser   *db.User
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{
			name: "hostile: invalid container id", id: "../etc",
			token:      adminToken,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "hostile: missing token", id: "c1",
			token:      func(t *testing.T) string { return "" },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "infra failure: inspect error", id: "c1",
			token:      adminToken,
			handler:    func(req *http.Request) (*http.Response, error) { return nil, assertErr("boom") },
			wantStatus: http.StatusNotFound,
		},
		{
			name: "hostile: excluded container (non-admin)", id: "c1",
			token:      func(t *testing.T) string { return issueTestAccessToken(t, &UserClaims{ID: 72, IsAdmin: false, PasswordVersion: 1, PasswordChanged: true}) },
			seedUser:   &db.User{ID: 72, Username: "wsUser72", Email: "wsuser72@test", IsActive: true, IsRestrictedAccess: true, AllowedContainers: ".*", PasswordVersion: 1, PasswordChanged: true},
			handler:    inspectHandler("secret-internal", "alpine", false),
			wantStatus: http.StatusNotFound,
		},
		{
			name: "hostile: non-admin unauthorized", id: "c1",
			token:      func(t *testing.T) string { return issueTestAccessToken(t, &UserClaims{ID: 70, IsAdmin: false, PasswordVersion: 1, PasswordChanged: true}) },
			seedUser:   &db.User{ID: 70, Username: "wsUser70", Email: "wsuser70@test", IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope", PasswordVersion: 1, PasswordChanged: true},
			handler:    inspectHandler("target", "alpine", false),
			wantStatus: http.StatusForbidden,
		},
	}

	origExcluded := excludedContainerNames
	excludedContainerNames = []string{"secret-internal"}
	t.Cleanup(func() { excludedContainerNames = origExcluded })

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true})
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			tok := tc.token(t)
			handler := tc.handler
			if handler == nil {
				handler = inspectHandler("target", "alpine", false)
			}
			cli := mockDockerClientWithRoundTripper(t, handler)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/ws/logs/"+tc.id, nil)
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			assert.NoError(t, handleGETWsLogsId(cli)(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

func TestHandleGETWsLogsId_UpgradeFailsAfterAuthorization(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1})
	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})
	cli := mockDockerClientWithRoundTripper(t, inspectHandler("target", "alpine", false))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/c1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("c1")
	assert.NoError(t, handleGETWsLogsId(cli)(c))
}

// logsStreamRoundTripper serves ContainerInspect + a raw multiplexed
// ContainerLogs stream containing a single log frame.
func logsStreamRoundTripper(containerName, image string, frame []byte) func(req *http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/json") && !strings.Contains(req.URL.Path, "/logs"):
			return makeResponse(http.StatusOK, `{"Id":"longid123","Name":"`+containerName+`","Config":{"Image":"`+image+`"}}`), nil
		case strings.Contains(req.URL.Path, "/logs"):
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(frame)), Header: make(http.Header)}, nil
		default:
			return makeResponse(http.StatusOK, `{}`), nil
		}
	}
}

func TestHandleGETWsLogsId_RealConnection_StreamsLogFrame(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true})
	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})

	origCheckOrigin := upgrader.CheckOrigin
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	t.Cleanup(func() { upgrader.CheckOrigin = origCheckOrigin })

	frame := dockerLogFrame(1, "hello from container\n")
	mockT := &mockDockerRoundTripper{handler: logsStreamRoundTripper("target", "alpine", frame)}
	cli, err := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	assert.NoError(t, err)

	e := echo.New()
	e.GET("/ws/logs/:id", handleGETWsLogsId(cli))
	srv := httptest.NewServer(e)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/logs/c1"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, wsBearerHeader(token))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	assert.NoError(t, err)
	assert.Equal(t, "hello from container\n", string(msg))
}

// ---- handleGETWsShellId ----

func TestHandleGETWsShellId_PreUpgradeBranches(t *testing.T) {
	adminToken := func(t *testing.T) string {
		return issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})
	}

	tests := []struct {
		name          string
		id            string
		shellQuery    string
		token         func(t *testing.T) string
		allowShell    bool
		canShellInDB  bool
		seedUser      *db.User
		handler       func(req *http.Request) (*http.Response, error)
		wantStatus    int
	}{
		{name: "hostile: invalid container id", id: "../etc", token: adminToken, allowShell: true, canShellInDB: true, wantStatus: http.StatusBadRequest},
		{name: "hostile: missing token", id: "c1", token: func(t *testing.T) string { return "" }, allowShell: true, canShellInDB: true, wantStatus: http.StatusUnauthorized},
		{name: "hostile: shell globally disabled", id: "c1", token: adminToken, allowShell: false, canShellInDB: true, wantStatus: http.StatusForbidden},
		{name: "hostile: user lacks can_shell in DB", id: "c1", token: adminToken, allowShell: true, canShellInDB: false, wantStatus: http.StatusForbidden},
		{
			name: "infra failure: inspect error", id: "c1", token: adminToken, allowShell: true, canShellInDB: true,
			handler: func(req *http.Request) (*http.Response, error) { return nil, assertErr("boom") }, wantStatus: http.StatusNotFound,
		},
		{
			name: "hostile: excluded container (non-admin)", id: "c1", allowShell: true, canShellInDB: true,
			token:    func(t *testing.T) string { return issueTestAccessToken(t, &UserClaims{ID: 73, IsAdmin: false, PasswordVersion: 1, PasswordChanged: true}) },
			seedUser: &db.User{ID: 73, Username: "wsUser73", Email: "wsuser73@test", IsActive: true, IsRestrictedAccess: true, AllowedContainers: ".*", PasswordVersion: 1, PasswordChanged: true, CanShell: true},
			handler:  inspectHandler("secret-internal", "alpine", false), wantStatus: http.StatusNotFound,
		},
		{
			name: "hostile: non-admin unauthorized", id: "c1", allowShell: true, canShellInDB: true,
			token:    func(t *testing.T) string { return issueTestAccessToken(t, &UserClaims{ID: 71, IsAdmin: false, PasswordVersion: 1, PasswordChanged: true}) },
			seedUser: &db.User{ID: 71, Username: "wsUser71", Email: "wsuser71@test", IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope", PasswordVersion: 1, PasswordChanged: true, CanShell: true},
			handler:  inspectHandler("target", "alpine", false), wantStatus: http.StatusForbidden,
		},
		{
			name: "hostile: invalid shell requested", id: "c1", shellQuery: "/bin/evil", token: adminToken, allowShell: true, canShellInDB: true,
			handler: inspectHandler("target", "alpine", false), wantStatus: http.StatusBadRequest,
		},
	}

	origExcluded := excludedContainerNames
	excludedContainerNames = []string{"secret-internal"}
	t.Cleanup(func() { excludedContainerNames = origExcluded })

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true, CanShell: tc.canShellInDB})
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			origAllowShell := AllowShell
			AllowShell = tc.allowShell
			t.Cleanup(func() { AllowShell = origAllowShell })

			tok := tc.token(t)
			handler := tc.handler
			if handler == nil {
				handler = inspectHandler("target", "alpine", false)
			}
			cli := mockDockerClientWithRoundTripper(t, handler)

			e := echo.New()
			target := "/ws/shell/" + tc.id
			if tc.shellQuery != "" {
				target += "?shell=" + tc.shellQuery
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			assert.NoError(t, handleGETWsShellId(cli)(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

func TestHandleGETWsShellId_UpgradeFailsAfterAuthorization(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, CanShell: true})
	origAllowShell := AllowShell
	AllowShell = true
	t.Cleanup(func() { AllowShell = origAllowShell })
	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})
	cli := mockDockerClientWithRoundTripper(t, inspectHandler("target", "alpine", false))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/ws/shell/c1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("c1")
	assert.NoError(t, handleGETWsShellId(cli)(c))
}

// shellExecRoundTripper serves ContainerInspect + an ExecCreate response
// (or failure). ExecAttach is NOT reachable through this RoundTripper since
// the real moby client hijacks a raw dialer connection for it instead —
// in this sandbox there is no docker socket, so ExecAttach fails naturally.
func shellExecRoundTripper(containerName, image string, execCreateFails bool) func(req *http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/json") && !strings.Contains(req.URL.Path, "/logs"):
			return makeResponse(http.StatusOK, `{"Id":"longid123","Name":"`+containerName+`","Config":{"Image":"`+image+`"}}`), nil
		case strings.HasSuffix(req.URL.Path, "/exec"):
			if execCreateFails {
				return nil, assertErr("exec create failed")
			}
			return makeResponse(http.StatusOK, `{"Id":"exec1"}`), nil
		default:
			return makeResponse(http.StatusOK, `{}`), nil
		}
	}
}

func TestHandleGETWsShellId_RealConnection_ExecCreateFails(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true, CanShell: true})
	origAllowShell := AllowShell
	AllowShell = true
	t.Cleanup(func() { AllowShell = origAllowShell })
	origCheckOrigin := upgrader.CheckOrigin
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	t.Cleanup(func() { upgrader.CheckOrigin = origCheckOrigin })

	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})
	mockT := &mockDockerRoundTripper{handler: shellExecRoundTripper("target", "alpine", true)}
	cli, err := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	assert.NoError(t, err)

	e := echo.New()
	e.GET("/ws/shell/:id", handleGETWsShellId(cli))
	srv := httptest.NewServer(e)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/shell/c1"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, wsBearerHeader(token))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	assert.NoError(t, err)
	assert.Contains(t, string(msg), "Failed to create terminal session")
}

func TestHandleGETWsShellId_RealConnection_ExecAttachFailsNoDockerSocket(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, Username: "wsAdmin1", IsAdmin: true, IsActive: true, PasswordVersion: 1, PasswordChanged: true, CanShell: true})
	origAllowShell := AllowShell
	AllowShell = true
	t.Cleanup(func() { AllowShell = origAllowShell })
	origCheckOrigin := upgrader.CheckOrigin
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	t.Cleanup(func() { upgrader.CheckOrigin = origCheckOrigin })

	token := issueTestAccessToken(t, &UserClaims{ID: 1, IsAdmin: true, PasswordVersion: 1, PasswordChanged: true})
	// ExecCreate succeeds via the mocked RoundTripper; ExecAttach dials the
	// real (nonexistent in this sandbox) docker socket directly and fails.
	mockT := &mockDockerRoundTripper{handler: shellExecRoundTripper("target", "alpine", false)}
	cli, err := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	assert.NoError(t, err)

	e := echo.New()
	e.GET("/ws/shell/:id", handleGETWsShellId(cli))
	srv := httptest.NewServer(e)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/shell/c1"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, wsBearerHeader(token))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	assert.NoError(t, err)
	assert.Contains(t, string(msg), "Failed to attach to terminal session")
}

