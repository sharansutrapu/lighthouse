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
	"golang.org/x/crypto/bcrypt"
	"lighthouse/db"
)

func TestHandlePOSTUserChangePassword_Table(t *testing.T) {
	tests := []struct {
		name       string
		userID     int
		skipSeed   bool
		seedUser   *db.User
		form       url.Values
		wantStatus int
	}{
		{name: "hostile: password too short", userID: 1, form: url.Values{"password": {"short"}}, wantStatus: http.StatusBadRequest},
		{name: "hostile: password too long", userID: 1, form: url.Values{"password": {strings.Repeat("a", 200)}}, wantStatus: http.StatusBadRequest},
		{name: "infra failure: user not found", userID: 999, skipSeed: true, form: url.Values{"password": {"longenoughpassword"}}, wantStatus: http.StatusUnauthorized},
		{
			name: "hostile: deactivated account", userID: 1,
			seedUser:   &db.User{ID: 1, Username: "deactivated", IsActive: false},
			form:       url.Values{"password": {"longenoughpassword"}},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "hostile: password already changed, missing current_password", userID: 1,
			seedUser:   &db.User{ID: 1, Username: "u1", IsActive: true, PasswordChanged: true},
			form:       url.Values{"password": {"longenoughpassword"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "hostile: password already changed, wrong current_password", userID: 1,
			seedUser:   &db.User{ID: 1, Username: "u1", IsActive: true, PasswordChanged: true, Password: hashedPasswordForTest("realpassword")},
			form:       url.Values{"password": {"longenoughpassword"}, "current_password": {"wrongpassword"}},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "happy path: first password change (PasswordChanged=false) skips current_password check", userID: 1,
			seedUser:   &db.User{ID: 1, Username: "u1", IsActive: true, PasswordChanged: false},
			form:       url.Values{"password": {"longenoughpassword"}},
			wantStatus: http.StatusOK,
		},
		{
			name: "happy path: correct current_password", userID: 1,
			seedUser:   &db.User{ID: 1, Username: "u1", IsActive: true, PasswordChanged: true, Password: hashedPasswordForTest("realpassword")},
			form:       url.Values{"password": {"longenoughpassword"}, "current_password": {"realpassword"}},
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if !tc.skipSeed && tc.seedUser != nil {
				// Capture BEFORE Create(), since GORM's `default:true` tag on
				// IsActive rewrites the in-memory struct on insert.
				wantActive := tc.seedUser.IsActive
				db.GormDB.Create(tc.seedUser)
				if !wantActive {
					db.GormDB.Model(&db.User{}).Where("id = ?", tc.seedUser.ID).Update("is_active", false)
				}
			}
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/user/change-password", strings.NewReader(tc.form.Encode()))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{ID: tc.userID})
			c.Set("user", token)

			assert.NoError(t, handlePOSTUserChangePassword()(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

func hashedPasswordForTest(plain string) string {
	h, _ := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	return string(h)
}
