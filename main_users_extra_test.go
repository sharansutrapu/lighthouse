package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func newAdminUsersRequest(form url.Values) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{ID: 1, Username: "admin"})
	c.Set("user", token)
	return c, rec
}

func TestHandlePOSTUsers_Table(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		skipRT     bool
		wantStatus int
	}{
		{
			name:       "hostile: missing role template and auth method for non-admin no-team user",
			form:       url.Values{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "hostile: admin requires auth method",
			form:       url.Values{"is_admin": {"true"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "hostile: invalid role template id",
			form:       url.Values{"authMethod": {"local"}, "role_template_id": {"9999"}, "username": {"x"}, "password": {"longenoughpassword"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "hostile: local auth missing username",
			form:       url.Values{"authMethod": {"local"}, "role_template_id": {"1"}, "password": {"longenoughpassword"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "hostile: local auth password too long",
			form:       url.Values{"authMethod": {"local"}, "role_template_id": {"1"}, "username": {"toolong"}, "password": {strings.Repeat("a", 200)}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "happy path: local user with role template",
			form:       url.Values{"authMethod": {"local"}, "role_template_id": {"1"}, "username": {"localuser1"}, "password": {"longenoughpassword"}},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "happy path: admin user skips role template lookup",
			form:       url.Values{"authMethod": {"local"}, "is_admin": {"true"}, "username": {"adminuser1"}, "password": {"longenoughpassword"}},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "happy path: user assigned to team skips role template lookup",
			form:       url.Values{"authMethod": {"local"}, "team_id": {"1"}, "username": {"teamuser1"}, "password": {"longenoughpassword"}},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "hostile: duplicate username fails create",
			form:       url.Values{"authMethod": {"local"}, "role_template_id": {"1"}, "username": {"duplicateuser"}, "password": {"longenoughpassword"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "hostile: invite auth missing email",
			form:       url.Values{"authMethod": {"invite"}, "role_template_id": {"1"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "happy path: invite auth creates pending invite",
			form:       url.Values{"authMethod": {"invite"}, "role_template_id": {"1"}, "email": {"invitee@example.com"}},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "hostile: unknown auth method",
			form:       url.Values{"authMethod": {"carrier-pigeon"}, "role_template_id": {"1"}},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			db.GormDB.Save(&db.Team{ID: 1, Name: "team1"})
			db.GormDB.Save(&db.RoleTemplate{ID: 1, Name: "Template1", CanStart: true})
			db.GormDB.Create(&db.User{Username: "duplicateuser", Password: "x", RoleTemplateID: nil})

			c, rec := newAdminUsersRequest(tc.form)
			h := handlePOSTUsers()
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

func TestHandlePOSTUsers_InviteWithSMTPConfigured(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.RoleTemplate{ID: 1, Name: "Template1"})
	db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Updates(map[string]interface{}{
		"smtp_host": "127.0.0.1", "smtp_port": 1025, "smtp_user": "u", "smtp_pass": "p",
	})

	form := url.Values{"authMethod": {"invite"}, "role_template_id": {"1"}, "email": {"invitee2@example.com"}}
	c, rec := newAdminUsersRequest(form)
	h := handlePOSTUsers()
	assert.NoError(t, h(c))
	assert.Equal(t, http.StatusCreated, rec.Code)

	var u db.User
	assert.NoError(t, db.GormDB.Where("email = ?", "invitee2@example.com").First(&u).Error)
	assert.NotEmpty(t, u.InviteToken)
	assert.NotNil(t, u.InviteExpiresAt)
}
