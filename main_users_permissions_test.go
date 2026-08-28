package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func newAdminScopedRequest(method, target string, form url.Values, id string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{ID: 1, Username: "admin"})
	c.Set("user", token)
	return c, rec
}

func TestHandleDELETEUsersId_Table(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		seedUser   *db.User
		wantStatus int
	}{
		{name: "hostile: invalid id format", id: "../etc", wantStatus: http.StatusBadRequest},
		{name: "hostile: cannot delete primary admin (id=1)", id: "1", seedUser: &db.User{ID: 1, Username: "primary"}, wantStatus: http.StatusBadRequest},
		{name: "hostile: nonexistent user id", id: "999", wantStatus: http.StatusBadRequest},
		{name: "happy path: deletes an existing non-primary user", id: "42", seedUser: &db.User{ID: 42, Username: "deleteme"}, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedUser != nil {
				db.GormDB.Create(tc.seedUser)
			}
			c, rec := newAdminScopedRequest(http.MethodDelete, "/users/"+tc.id, url.Values{}, tc.id)
			assert.NoError(t, handleDELETEUsersId()(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

func TestHandlePUTUsersIdPermissions_Table(t *testing.T) {
	t.Run("hostile: invalid id format", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		c, rec := newAdminScopedRequest(http.MethodPut, "/users/x/permissions", url.Values{}, "../etc")
		assert.NoError(t, handlePUTUsersIdPermissions()(c))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("happy path: team_id=null clears the team association", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		team := db.Team{Name: "some-team"}
		db.GormDB.Create(&team)
		teamID := team.ID
		user := db.User{ID: 55, Username: "teamuser", TeamID: &teamID}
		db.GormDB.Create(&user)

		f := url.Values{"team_id": {"null"}}
		c, rec := newAdminScopedRequest(http.MethodPut, "/users/55/permissions", f, "55")
		assert.NoError(t, handlePUTUsersIdPermissions()(c))
		assert.Equal(t, http.StatusOK, rec.Code)

		var updated db.User
		assert.NoError(t, db.GormDB.First(&updated, 55).Error)
		assert.Nil(t, updated.TeamID)
	})

	t.Run("happy path: team_id set to a valid team id", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		team := db.Team{Name: "new-team"}
		db.GormDB.Create(&team)
		db.GormDB.Create(&db.User{ID: 56, Username: "teamuser2"})

		f := url.Values{"team_id": {strconv.Itoa(int(team.ID))}}
		c, rec := newAdminScopedRequest(http.MethodPut, "/users/56/permissions", f, "56")
		assert.NoError(t, handlePUTUsersIdPermissions()(c))
		assert.Equal(t, http.StatusOK, rec.Code)

		var updated db.User
		assert.NoError(t, db.GormDB.First(&updated, 56).Error)
		assert.NotNil(t, updated.TeamID)
		assert.Equal(t, team.ID, *updated.TeamID)
	})
}
