package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2/google"
	"lighthouse/db"
)

// fakeGoogleTransport intercepts every outbound HTTP request made during the
// Google OAuth callback flow (token exchange + userinfo lookup) so the test
// never touches the real network. Routes by path substring since both calls
// go through http.DefaultTransport once google.Endpoint is overridden.
type fakeGoogleTransport struct {
	tokenStatus    int
	tokenBody      string
	userinfoStatus int
	userinfoBody   string
	failToken      bool
	failUserinfo   bool
}

func (f *fakeGoogleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/token") {
		if f.failToken {
			return nil, fmt.Errorf("simulated token exchange network failure")
		}
		return &http.Response{
			StatusCode: f.tokenStatus,
			Body:       io.NopCloser(strings.NewReader(f.tokenBody)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	if strings.Contains(req.URL.Path, "/userinfo") {
		if f.failUserinfo {
			return nil, fmt.Errorf("simulated userinfo network failure")
		}
		return &http.Response{
			StatusCode: f.userinfoStatus,
			Body:       io.NopCloser(strings.NewReader(f.userinfoBody)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	return nil, fmt.Errorf("unexpected outbound request to %s", req.URL.String())
}

// withFakeGoogle wires google.Endpoint + http.DefaultTransport to the fake
// transport for the duration of one test, restoring both afterwards.
func withFakeGoogle(t *testing.T, ft *fakeGoogleTransport) {
	t.Helper()
	origEndpoint := google.Endpoint
	origTransport := http.DefaultTransport
	google.Endpoint.AuthURL = "https://fake-google.test/auth"
	google.Endpoint.TokenURL = "https://fake-google.test/token"
	http.DefaultTransport = ft
	t.Cleanup(func() {
		google.Endpoint = origEndpoint
		http.DefaultTransport = origTransport
	})
}

func validTokenJSON() string {
	return `{"access_token":"fake-access-token","token_type":"Bearer","expires_in":3600}`
}

func buildCallbackRequest(state, code string) (*httptest.ResponseRecorder, echo.Context, *echo.Echo) {
	e := echo.New()
	target := "/auth/google/callback?state=" + state
	if code != "" {
		target += "&code=" + code
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if state != "" {
		req.AddCookie(&http.Cookie{Name: "oauth_state", Value: state})
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return rec, c, e
}

// TestHandleGETAuthGoogleCallback_Table drives the callback handler through
// every branch: state validation, token exchange, userinfo fetch, first-user
// bootstrap, invite consumption, existing-user google-id binding/mismatch,
// deactivated accounts, and all associated error redirects.
func TestHandleGETAuthGoogleCallback_Table(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		noCookie   bool
		code       string
		ft         *fakeGoogleTransport
		seed       func()
		wantStatus int
		wantLoc    string // substring expected in Location header (redirects) or body
	}{
		{
			name:       "hostile: missing oauth_state cookie",
			state:      "abc",
			noCookie:   true,
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "Invalid+OAuth+state",
		},
		{
			name:       "hostile: state query param does not match cookie",
			state:      "abc",
			code:       "irrelevant",
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "",
		},
		{
			name: "infra failure: token exchange fails",
			state: "state1",
			code:  "somecode",
			ft:    &fakeGoogleTransport{failToken: true},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "Failed+to+exchange+token",
		},
		{
			name:  "infra failure: token exchange returns non-2xx",
			state: "state2",
			code:  "somecode",
			ft:    &fakeGoogleTransport{tokenStatus: http.StatusBadRequest, tokenBody: `{"error":"invalid_grant"}`},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "Failed+to+exchange+token",
		},
		{
			name:  "infra failure: userinfo request fails",
			state: "state3",
			code:  "somecode",
			ft:    &fakeGoogleTransport{tokenStatus: 200, tokenBody: validTokenJSON(), failUserinfo: true},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "Failed+to+get+user+info",
		},
		{
			name:  "hostile: userinfo returns malformed JSON",
			state: "state4",
			code:  "somecode",
			ft:    &fakeGoogleTransport{tokenStatus: 200, tokenBody: validTokenJSON(), userinfoStatus: 200, userinfoBody: `{not-json`},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "Failed+to+parse+user+info",
		},
		{
			name:  "happy path: first user bootstraps as admin",
			state: "state5",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-1","email":"first@example.com","name":"First User"}`,
			},
			seed:       func() {},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "/?code=",
		},
		{
			name:  "hostile: not first user and not invited -> rejected",
			state: "state6",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-2","email":"uninvited@example.com","name":"Nobody"}`,
			},
			seed: func() {
				db.GormDB.Create(&db.User{Username: "existing-admin", Email: "admin@example.com", IsAdmin: true, IsActive: true})
			},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "must+be+invited",
		},
		{
			name:  "happy path: existing user with matching google id logs in",
			state: "state7",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-existing","email":"match@example.com","name":"Match User"}`,
			},
			seed: func() {
				db.GormDB.Create(&db.User{Username: "match", Email: "match@example.com", GoogleID: "g-existing", IsActive: true})
			},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "/?code=",
		},
		{
			name:  "happy path: existing user first-time google binding (empty google id)",
			state: "state8",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-new-bind","email":"bindme@example.com","name":"Bind Me"}`,
			},
			seed: func() {
				db.GormDB.Create(&db.User{Username: "bindme", Email: "bindme@example.com", GoogleID: "", IsActive: true})
			},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "/?code=",
		},
		{
			name:  "hostile: existing user google id mismatch",
			state: "state9",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-attacker","email":"victim@example.com","name":"Victim"}`,
			},
			seed: func() {
				db.GormDB.Create(&db.User{Username: "victim", Email: "victim@example.com", GoogleID: "g-legit-owner", IsActive: true})
			},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "does+not+match",
		},
		{
			name:  "hostile: deactivated account",
			state: "state10",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-deact","email":"deact@example.com","name":"Deact"}`,
			},
			seed: func() {
				u := db.User{Username: "deact", Email: "deact@example.com", GoogleID: "g-deact", IsActive: true}
				db.GormDB.Create(&u)
				// GORM's `default:true` tag on IsActive means an explicit
				// false at Create time is silently coerced back to true;
				// a targeted column UPDATE bypasses default substitution.
				db.GormDB.Model(&u).Update("is_active", false)
			},
			wantStatus: http.StatusForbidden,
			wantLoc:    "Account deactivated",
		},
		{
			name:  "happy path: invite token consumed successfully",
			state: "state11:invite-tok-123",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-invitee","email":"invitee@example.com","name":"Invitee"}`,
			},
			seed: func() {
				future := time.Now().Add(24 * time.Hour)
				db.GormDB.Create(&db.User{Username: "invitee", Email: "invitee@example.com", InviteToken: "invite-tok-123", InviteExpiresAt: &future, IsActive: true})
			},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "/?code=",
		},
		{
			name:  "hostile: invite token mismatch",
			state: "state12:wrong-token",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-invitee2","email":"invitee2@example.com","name":"Invitee2"}`,
			},
			seed: func() {
				future := time.Now().Add(24 * time.Hour)
				db.GormDB.Create(&db.User{Username: "invitee2", Email: "invitee2@example.com", InviteToken: "correct-token", InviteExpiresAt: &future, IsActive: true})
			},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "Invalid+invite+token",
		},
		{
			name:  "hostile: invite token expired",
			state: "state13:expired-tok",
			code:  "somecode",
			ft: &fakeGoogleTransport{
				tokenStatus: 200, tokenBody: validTokenJSON(),
				userinfoStatus: 200, userinfoBody: `{"id":"g-invitee3","email":"invitee3@example.com","name":"Invitee3"}`,
			},
			seed: func() {
				past := time.Now().Add(-24 * time.Hour)
				db.GormDB.Create(&db.User{Username: "invitee3", Email: "invitee3@example.com", InviteToken: "expired-tok", InviteExpiresAt: &past, IsActive: true})
			},
			wantStatus: http.StatusTemporaryRedirect,
			wantLoc:    "invite+link+has+expired",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seed != nil {
				tc.seed()
			}
			if tc.ft != nil {
				withFakeGoogle(t, tc.ft)
			}

			state := tc.state
			cookieState := state
			if tc.noCookie {
				cookieState = ""
			}
			rec, c, _ := buildCallbackRequest(cookieState, tc.code)
			if tc.noCookie {
				// Re-issue the request without a cookie but with a state query param.
				req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state="+state+"&code="+tc.code, nil)
				rec = httptest.NewRecorder()
				e := echo.New()
				c = e.NewContext(req, rec)
			}

			h := handleGETAuthGoogleCallback()
			err := h(c)
			assert.NoError(t, err)
			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantLoc != "" {
				loc := rec.Header().Get("Location")
				if loc == "" {
					loc = rec.Body.String()
				}
				assert.Contains(t, loc, tc.wantLoc)
			}
		})
	}
}

// TestHandleGETAuthGoogleCallback_BootstrapCreateFails covers the rare path
// where the first-user bootstrap Create() call itself fails.
func TestHandleGETAuthGoogleCallback_BootstrapCreateFails(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	// Force the upcoming Create to fail by dropping the users table so the
	// initial SELECT (Where email) also fails, but with a real DB error
	// distinct from ErrRecordNotFound -- covers "Internal database error".
	db.GormDB.Migrator().DropTable(&db.User{})

	ft := &fakeGoogleTransport{
		tokenStatus: 200, tokenBody: validTokenJSON(),
		userinfoStatus: 200, userinfoBody: `{"id":"g-x","email":"x@example.com","name":"X"}`,
	}
	withFakeGoogle(t, ft)

	rec, c, _ := buildCallbackRequest("bootstrapstate", "somecode")
	h := handleGETAuthGoogleCallback()
	err := h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "Internal+database+error")
}

// TestGenerateSecureCode_Uniqueness covers generateSecureCode's normal path
// beyond the single call other tests happen to make.
func TestGenerateSecureCode_Uniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		code := generateSecureCode()
		assert.Len(t, code, 64) // 32 bytes hex-encoded
		assert.False(t, seen[code], "generateSecureCode produced a duplicate")
		seen[code] = true
	}
}
