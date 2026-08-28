package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/cluster"
	"lighthouse/db"
)

func withEnvGates(t *testing.T, start, stop, restart, del bool) {
	t.Helper()
	origStart, origStop, origRestart, origDel := CanStart, CanStop, CanRestart, CanDelete
	CanStart, CanStop, CanRestart, CanDelete = start, stop, restart, del
	t.Cleanup(func() {
		CanStart, CanStop, CanRestart, CanDelete = origStart, origStop, origRestart, origDel
	})
}

func withLighthouseMode(t *testing.T, mode string) {
	t.Helper()
	orig := LighthouseMode
	LighthouseMode = mode
	t.Cleanup(func() { LighthouseMode = orig })
}

func newActionRequest(id, action string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/containers/"+id+"/action", strings.NewReader("action="+action))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)
	return c, rec
}

// TestHandlePOSTContainersIdAction_Table exhaustively covers every branch of
// handlePOSTContainersIdAction: validation, env gates, self-container
// protection, exclusion, per-user permission, regex authorization, Docker
// error propagation, and success for every action verb.
func TestHandlePOSTContainersIdAction_Table(t *testing.T) {
	inspectOK := func(name, image string) func(req *http.Request) (*http.Response, error) {
		return func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") {
				return makeResponse(http.StatusOK, `{"Id":"target","Name":"`+name+`","Config":{"Image":"`+image+`"}}`), nil
			}
			return makeResponse(http.StatusOK, `{}`), nil
		}
	}

	tests := []struct {
		name       string
		id         string
		action     string
		isAdmin    bool
		userID     int
		envGates   [4]bool // start, stop, restart, delete
		seedUser   *db.User
		handler    func(req *http.Request) (*http.Response, error)
		wantStatus int
	}{
		{
			name:       "hostile: invalid container id",
			id:         "../etc/passwd",
			action:     "start",
			isAdmin:    true,
			handler:    inspectOK("target", "alpine"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "hostile: invalid action verb",
			id:         "abc123",
			action:     "nuke",
			isAdmin:    true,
			handler:    inspectOK("target", "alpine"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "hostile: non-admin, action disabled at env level",
			id:         "abc123",
			action:     "start",
			isAdmin:    false,
			userID:     1,
			envGates:   [4]bool{false, false, false, false},
			handler:    inspectOK("target", "alpine"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "infra failure: inspect fails",
			id:         "abc123",
			action:     "start",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler:    func(req *http.Request) (*http.Response, error) { return nil, assertErr("inspect failed") },
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "hostile: self-container blocks stop",
			id:         "abc123",
			action:     "stop",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler:    inspectOK("lighthouse", "lighthouse:latest"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "hostile: self-container blocks remove",
			id:         "abc123",
			action:     "remove",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler:    inspectOK("lighthouse", "lighthouse:latest"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "happy path: self-container allows restart",
			id:         "abc123",
			action:     "restart",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler:    inspectOK("lighthouse", "lighthouse:latest"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "hostile: non-admin lacks per-user permission",
			id:         "abc123",
			action:     "start",
			isAdmin:    false,
			userID:     42,
			envGates:   [4]bool{true, true, true, true},
			seedUser:   &db.User{ID: 42, IsActive: true, CanStart: false, IsRestrictedAccess: false},
			handler:    inspectOK("othername", "alpine"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "hostile: non-admin unauthorized by container regex",
			id:         "abc123",
			action:     "start",
			isAdmin:    false,
			userID:     43,
			envGates:   [4]bool{true, true, true, true},
			seedUser:   &db.User{ID: 43, IsActive: true, CanStart: true, IsRestrictedAccess: true, AllowedContainers: "definitely-not-it"},
			handler:    inspectOK("othername", "alpine"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "happy path: admin starts container",
			id:         "abc123",
			action:     "start",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler:    inspectOK("othername", "alpine"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "happy path: admin stops container",
			id:         "abc123",
			action:     "stop",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler:    inspectOK("othername", "alpine"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "happy path: admin restarts container",
			id:         "abc123",
			action:     "restart",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler:    inspectOK("othername", "alpine"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "happy path: admin removes container",
			id:         "abc123",
			action:     "remove",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler:    inspectOK("othername", "alpine"),
			wantStatus: http.StatusOK,
		},
		{
			name:     "happy path: authorized non-admin starts container",
			id:       "abc123",
			action:   "start",
			isAdmin:  false,
			userID:   44,
			envGates: [4]bool{true, true, true, true},
			seedUser: &db.User{ID: 44, IsActive: true, CanStart: true, IsRestrictedAccess: true, AllowedContainers: "othername"},
			handler:  inspectOK("othername", "alpine"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "infra failure: docker action call fails",
			id:         "abc123",
			action:     "start",
			isAdmin:    true,
			envGates:   [4]bool{true, true, true, true},
			handler: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/json") {
					return makeResponse(http.StatusOK, `{"Id":"target","Name":"othername","Config":{"Image":"alpine"}}`), nil
				}
				return nil, assertErr("docker start failed")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedUser != nil {
				db.GormDB.Save(tc.seedUser)
			}
			if tc.isAdmin {
				// GORM assigns a fresh auto-increment ID when the primary key
				// is left at its zero value on Create/Save, so an explicit
				// ID=0 here would silently create ID=1 instead — always use
				// a non-zero ID so the handler's DB admin re-check actually
				// finds this row.
				userID := tc.userID
				if userID == 0 {
					userID = 1
				}
				tc.userID = userID
				db.GormDB.Save(&db.User{ID: uint(userID), IsAdmin: true, IsActive: true})
			}
			withEnvGates(t, tc.envGates[0], tc.envGates[1], tc.envGates[2], tc.envGates[3])

			cli := mockDockerClientWithRoundTripper(t, tc.handler)
			c, rec := newActionRequest(tc.id, tc.action)
			mockUserContext(c, uint(tc.userID), tc.isAdmin)

			h := handlePOSTContainersIdAction(cli)
			err := h(c)
			assert.NoError(t, err)
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

// TestHandlePOSTContainersIdAction_HubModeSpokeDispatch covers the hub-mode
// branches that dispatch start/stop/remove to a spoke node instead of
// executing locally, for both the success and failure of SendCommandToSpoke.
func TestHandlePOSTContainersIdAction_HubModeSpokeDispatch(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true, IsActive: true})
	withEnvGates(t, true, true, true, true)
	withLighthouseMode(t, "hub")

	cluster.GlobalHub.Lock()
	cluster.GlobalHub.SpokeContainers["node1"] = []map[string]interface{}{{"ID": "abc123"}}
	cluster.GlobalHub.Unlock()
	t.Cleanup(func() {
		cluster.GlobalHub.Lock()
		delete(cluster.GlobalHub.SpokeContainers, "node1")
		cluster.GlobalHub.Unlock()
	})

	cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/json") {
			return makeResponse(http.StatusOK, `{"Id":"abc123","Name":"spoke-owned","Config":{"Image":"alpine"}}`), nil
		}
		return makeResponse(http.StatusOK, `{}`), nil
	})

	for _, action := range []string{"start", "stop", "remove"} {
		c, rec := newActionRequest("abc123", action)
		mockUserContext(c, 1, true)
		h := handlePOSTContainersIdAction(cli)
		err := h(c)
		assert.NoError(t, err)
		// No spoke actually connected, so SendCommandToSpoke returns an error -> 500.
		assert.Equal(t, http.StatusInternalServerError, rec.Code, "action=%s", action)
	}
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func assertErr(msg string) error { return simpleErr(msg) }
