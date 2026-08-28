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
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"lighthouse/alerts"
	"lighthouse/db"
)

func mockClaimsContext(c echo.Context, claims *UserClaims) {
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	c.Set("user", token)
}

func newFormRequest(method, target string, form url.Values) (*echo.Echo, echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return e, c, rec
}

func newJSONRequestG(method, target, body string) (*echo.Echo, echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return e, c, rec
}

func newTestAlertManager(t *testing.T) *alerts.AlertManager {
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, err := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	assert.NoError(t, err)
	return alerts.NewAlertManager(cli)
}

// ---- handlePOSTAlertsRules ----

func TestHandlePOSTAlertsRules_Table(t *testing.T) {
	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
	}{
		{name: "hostile: empty name", form: url.Values{}, wantStatus: http.StatusBadRequest},
		{name: "hostile: invalid container_pattern regex", form: url.Values{"name": {"r1"}, "container_pattern": {"(unterminated"}}, wantStatus: http.StatusBadRequest},
		{name: "hostile: invalid log_pattern regex", form: url.Values{"name": {"r1"}, "log_pattern": {"(unterminated"}}, wantStatus: http.StatusBadRequest},
		{
			name: "happy path: full create with custom cooldown and channels",
			form: url.Values{
				"name": {"my-rule"}, "container_pattern": {"^web.*$"}, "log_pattern": {"error"},
				"event_types": {"start,stop"}, "enabled": {"true"}, "cooldown_seconds": {"60"},
				"enable_slack": {"true"}, "enable_msteams": {"true"}, "enable_gchat": {"true"},
				"enable_generic_webhook": {"true"}, "enable_email": {"true"}, "email_address": {"a@b.com"},
				"metric_cpu_threshold": {"80.5"}, "metric_mem_threshold": {"90"},
			},
			wantStatus: http.StatusCreated,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
			am := newTestAlertManager(t)
			_, c, rec := newFormRequest(http.MethodPost, "/alerts/rules", tc.form)
			mockUserContext(c, 1, true)
			h := handlePOSTAlertsRules(am)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus == http.StatusCreated {
				var r alerts.AlertRule
				assert.NoError(t, db.GormDB.Where("name = ?", "my-rule").First(&r).Error)
				assert.Equal(t, "my-rule", r.Name)
				assert.Equal(t, 60, r.CooldownSeconds)
				assert.True(t, r.EnableSlack)
			}
		})
	}
}

// ---- handlePUTAlertsRulesId ----

func TestHandlePUTAlertsRulesId_Table(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		skipSeed   bool
		form       url.Values
		wantStatus int
	}{
		{name: "hostile: invalid id", id: "../etc", form: url.Values{"name": {"x"}}, wantStatus: http.StatusBadRequest},
		{name: "infra failure: rule not found", id: "999", form: url.Values{"name": {"x"}}, skipSeed: true, wantStatus: http.StatusNotFound},
		{name: "hostile: empty name", id: "1", form: url.Values{}, wantStatus: http.StatusBadRequest},
		{name: "hostile: invalid container_pattern regex", id: "1", form: url.Values{"name": {"x"}, "container_pattern": {"(bad"}}, wantStatus: http.StatusBadRequest},
		{name: "hostile: invalid log_pattern regex", id: "1", form: url.Values{"name": {"x"}, "log_pattern": {"(bad"}}, wantStatus: http.StatusBadRequest},
		{name: "happy path: update success", id: "1", form: url.Values{"name": {"updated-rule"}, "cooldown_seconds": {"120"}, "enabled": {"true"}}, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
			if !tc.skipSeed {
				db.GormDB.Create(&alerts.AlertRule{Name: "original"})
			}
			am := newTestAlertManager(t)
			_, c, rec := newFormRequest(http.MethodPut, "/alerts/rules/"+tc.id, tc.form)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockUserContext(c, 1, true)
			h := handlePUTAlertsRulesId(am)
			assert.NoError(t, h(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus == http.StatusOK {
				var r alerts.AlertRule
				assert.NoError(t, db.GormDB.First(&r, 1).Error)
				assert.Equal(t, "updated-rule", r.Name)
				assert.Equal(t, 120, r.CooldownSeconds)
			}
		})
	}
}

func TestHandleDELETEAlertsRulesId_Extra(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	r := alerts.AlertRule{Name: "to-delete"}
	db.GormDB.Create(&r)
	am := newTestAlertManager(t)

	_, c, rec := newFormRequest(http.MethodDelete, "/alerts/rules/../bad", nil)
	c.SetParamNames("id")
	c.SetParamValues("../bad")
	mockUserContext(c, 1, true)
	assert.NoError(t, handleDELETEAlertsRulesId(am)(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	idStr := strconv.FormatInt(r.ID, 10)
	_, c2, rec2 := newFormRequest(http.MethodDelete, "/alerts/rules/"+idStr, nil)
	c2.SetParamNames("id")
	c2.SetParamValues(idStr)
	mockUserContext(c2, 1, true)
	assert.NoError(t, handleDELETEAlertsRulesId(am)(c2))
	assert.Equal(t, http.StatusOK, rec2.Code)
	var remaining alerts.AlertRule
	assert.Error(t, db.GormDB.First(&remaining, r.ID).Error)
}

func TestHandlePUTAlertsRulesIdToggle_Table(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		form        url.Values
		wantStatus  int
		wantEnabled bool
	}{
		{name: "hostile: invalid id", id: "../bad", form: url.Values{}, wantStatus: http.StatusBadRequest},
		{name: "happy path: enabled=true", id: "1", form: url.Values{"enabled": {"true"}}, wantStatus: http.StatusOK, wantEnabled: true},
		{name: "happy path: omitted defaults enabled true", id: "1", form: url.Values{}, wantStatus: http.StatusOK, wantEnabled: true},
		{name: "happy path: enabled=false", id: "1", form: url.Values{"enabled": {"false"}}, wantStatus: http.StatusOK, wantEnabled: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
			db.GormDB.Create(&alerts.AlertRule{Name: "r1"})
			am := newTestAlertManager(t)
			_, c, rec := newFormRequest(http.MethodPut, "/alerts/rules/"+tc.id+"/toggle", tc.form)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockUserContext(c, 1, true)
			assert.NoError(t, handlePUTAlertsRulesIdToggle(am)(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus == http.StatusOK {
				var r alerts.AlertRule
				assert.NoError(t, db.GormDB.First(&r, 1).Error)
				assert.Equal(t, tc.wantEnabled, r.Enabled)
			}
		})
	}
}

func TestHandlePOSTAlertsRulesBulkChannels_Table(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "hostile: invalid JSON", body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "hostile: no rules selected", body: `{"rule_ids":[]}`, wantStatus: http.StatusBadRequest},
		{name: "happy path: bulk update", body: `{"rule_ids":[1,2],"enable_slack":true,"enable_email":true}`, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
			db.GormDB.Create(&alerts.AlertRule{Name: "r1"})
			db.GormDB.Create(&alerts.AlertRule{Name: "r2"})
			am := newTestAlertManager(t)
			_, c, rec := newJSONRequestG(http.MethodPost, "/alerts/rules/bulk-channels", tc.body)
			mockUserContext(c, 1, true)
			assert.NoError(t, handlePOSTAlertsRulesBulkChannels(am)(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus == http.StatusOK {
				var r alerts.AlertRule
				assert.NoError(t, db.GormDB.First(&r, 1).Error)
				assert.True(t, r.EnableSlack)
				assert.True(t, r.EnableEmail)
			}
		})
	}
}

func TestHandleDELETEAlertsHistory_Extra(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Create(&db.AlertHistory{RuleName: "r1"})
	db.GormDB.Create(&db.AlertHistory{RuleName: "r2"})
	_, c, rec := newFormRequest(http.MethodDelete, "/alerts/history", nil)
	mockUserContext(c, 1, true)
	assert.NoError(t, handleDELETEAlertsHistory()(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	var count int64
	db.GormDB.Model(&db.AlertHistory{}).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestHandleDELETEAlertsHistory_DBErrorReturns500(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Migrator().DropTable(&db.AlertHistory{})
	_, c, rec := newFormRequest(http.MethodDelete, "/alerts/history", nil)
	mockUserContext(c, 1, true)
	assert.NoError(t, handleDELETEAlertsHistory()(c))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleGETAlertsHistory_Table(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantCount int
	}{
		{name: "happy path: default limit", query: "", wantCount: 3},
		{name: "happy path: custom limit", query: "?limit=1", wantCount: 1},
		{name: "hostile: over-max limit ignored, falls back to default", query: "?limit=9999", wantCount: 3},
		{name: "happy path: rule_id filter", query: "?rule_id=1", wantCount: 1},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			one := uint(1)
			two := uint(2)
			db.GormDB.Create(&db.AlertHistory{RuleName: "r1", RuleID: &one})
			db.GormDB.Create(&db.AlertHistory{RuleName: "r2", RuleID: &two})
			db.GormDB.Create(&db.AlertHistory{RuleName: "r3", RuleID: &two})
			_, c, rec := newFormRequest(http.MethodGet, "/alerts/history"+tc.query, nil)
			mockUserContext(c, 1, true)
			assert.NoError(t, handleGETAlertsHistory()(c))
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

// ---- GitOps handlers ----

func TestHandleGETGitopsProjects_Table(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Create(&db.GitProject{Name: "web-app"})
	db.GormDB.Create(&db.GitProject{Name: "secret-internal"})

	t.Run("happy path: admin sees all projects", func(t *testing.T) {
		_, c, rec := newFormRequest(http.MethodGet, "/gitops/projects", nil)
		mockClaimsContext(c, &UserClaims{ID: 1, IsAdmin: true})
		assert.NoError(t, handleGETGitopsProjects()(c))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "secret-internal")
	})

	t.Run("hostile: non-admin sees only authorized projects", func(t *testing.T) {
		db.GormDB.Save(&db.User{ID: 60, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "web-app"})
		_, c, rec := newFormRequest(http.MethodGet, "/gitops/projects", nil)
		mockClaimsContext(c, &UserClaims{ID: 60, IsAdmin: false})
		assert.NoError(t, handleGETGitopsProjects()(c))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "web-app")
		assert.NotContains(t, rec.Body.String(), "secret-internal")
	})
}

func TestHandlePOSTGitopsProjects_Table(t *testing.T) {
	tests := []struct {
		name       string
		claims     *UserClaims
		body       string
		wantStatus int
	}{
		{name: "hostile: non-admin lacks create permission", claims: &UserClaims{ID: 1, IsAdmin: false}, body: `{}`, wantStatus: http.StatusForbidden},
		{name: "hostile: invalid JSON payload", claims: &UserClaims{ID: 1, IsAdmin: true}, body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "hostile: branch starts with dash", claims: &UserClaims{ID: 1, IsAdmin: true}, body: `{"branch":"-x","source_type":"git","repo_url":"https://example.com/r.git"}`, wantStatus: http.StatusBadRequest},
		{name: "hostile: repo_url starts with dash", claims: &UserClaims{ID: 1, IsAdmin: true}, body: `{"repo_url":"-x","source_type":"git"}`, wantStatus: http.StatusBadRequest},
		{name: "hostile: repo_url uses hostile transport-helper scheme", claims: &UserClaims{ID: 1, IsAdmin: true}, body: `{"repo_url":"ext::sh -c evil","source_type":"git"}`, wantStatus: http.StatusBadRequest},
		{name: "hostile: compose path traversal", claims: &UserClaims{ID: 1, IsAdmin: true}, body: `{"repo_url":"https://example.com/r.git","source_type":"git","compose_path":"../../etc/passwd"}`, wantStatus: http.StatusBadRequest},
		{name: "hostile: compose path absolute", claims: &UserClaims{ID: 1, IsAdmin: true}, body: `{"repo_url":"https://example.com/r.git","source_type":"git","compose_path":"/etc/passwd"}`, wantStatus: http.StatusBadRequest},
		{name: "happy path: inline source skips repo URL validation", claims: &UserClaims{ID: 1, IsAdmin: true}, body: `{"name":"inline-proj","source_type":"inline","compose_content":"version: '3'"}`, wantStatus: http.StatusOK},
		{name: "happy path: git source, authorized non-admin with CanCreateDeployments", claims: &UserClaims{ID: 1, IsAdmin: false, CanCreateDeployments: true}, body: `{"name":"git-proj","source_type":"git","repo_url":"https://example.com/r.git","branch":"main","compose_path":"docker-compose.yml"}`, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			_, c, rec := newJSONRequestG(http.MethodPost, "/gitops/projects", tc.body)
			mockClaimsContext(c, tc.claims)
			assert.NoError(t, handlePOSTGitopsProjects()(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus == http.StatusOK {
				var p db.GitProject
				assert.NoError(t, db.GormDB.First(&p).Error)
				assert.Equal(t, "pending", p.Status)
			}
		})
	}
}

func TestHandlePUTGitopsProjectsId_Table(t *testing.T) {
	tests := []struct {
		name        string
		claims      *UserClaims
		id          string
		skipSeed    bool
		body        string
		seedProject *db.GitProject
		wantStatus  int
	}{
		{name: "hostile: non-admin lacks edit permission", claims: &UserClaims{ID: 1, IsAdmin: false}, id: "1", body: `{}`, wantStatus: http.StatusForbidden},
		{name: "hostile: invalid id", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "../etc", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "infra failure: project not found", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "999", body: `{}`, skipSeed: true, wantStatus: http.StatusNotFound},
		{
			name: "hostile: non-admin unauthorized by pattern", claims: &UserClaims{ID: 61, IsAdmin: false, CanEditDeployments: true}, id: "1", body: `{"name":"x"}`,
			seedProject: &db.GitProject{Name: "restricted-proj", SourceType: "git"}, wantStatus: http.StatusForbidden,
		},
		{name: "hostile: branch starts with dash", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "1", body: `{"branch":"-x"}`, seedProject: &db.GitProject{Name: "p1", SourceType: "git"}, wantStatus: http.StatusBadRequest},
		{name: "hostile: invalid repo_url", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "1", body: `{"repo_url":"ext::evil"}`, seedProject: &db.GitProject{Name: "p1", SourceType: "git"}, wantStatus: http.StatusBadRequest},
		{name: "hostile: compose path traversal", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "1", body: `{"compose_path":"../x"}`, seedProject: &db.GitProject{Name: "p1", SourceType: "git"}, wantStatus: http.StatusBadRequest},
		{
			name: "happy path: admin updates git project", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "1",
			body:        `{"name":"renamed","repo_url":"https://example.com/new.git","branch":"dev","compose_path":"a/b.yml"}`,
			seedProject: &db.GitProject{Name: "p1", SourceType: "git", AuthToken: "old-token"},
			wantStatus:  http.StatusOK,
		},
		{
			name: "happy path: inline project updates compose_content, clears empty auth token", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "1",
			body:        `{"name":"renamed-inline","compose_content":"version: '3.9'"}`,
			seedProject: &db.GitProject{Name: "p1", SourceType: "inline", AuthToken: "old-token"},
			wantStatus:  http.StatusOK,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedProject != nil {
				db.GormDB.Create(tc.seedProject)
			}
			if tc.claims.ID == 61 {
				db.GormDB.Save(&db.User{ID: 61, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"})
			}
			_, c, rec := newJSONRequestG(http.MethodPut, "/gitops/projects/"+tc.id, tc.body)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockClaimsContext(c, tc.claims)
			assert.NoError(t, handlePUTGitopsProjectsId()(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus == http.StatusOK {
				var p db.GitProject
				assert.NoError(t, db.GormDB.First(&p, 1).Error)
				assert.Equal(t, "pending", p.Status)
				if p.SourceType == "inline" {
					assert.Equal(t, "version: '3.9'", p.ComposeContent)
					assert.Equal(t, "", p.AuthToken)
				} else {
					assert.Equal(t, "https://example.com/new.git", p.RepoURL)
					assert.Equal(t, "dev", p.Branch)
				}
			}
		})
	}
}

func TestHandlePOSTGitopsProjectsIdSync_Table(t *testing.T) {
	tests := []struct {
		name        string
		claims      *UserClaims
		id          string
		seedProject *db.GitProject
		wantStatus  int
	}{
		{name: "hostile: non-admin lacks permission", claims: &UserClaims{ID: 1, IsAdmin: false}, id: "1", wantStatus: http.StatusForbidden},
		{name: "hostile: invalid id", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "../x", wantStatus: http.StatusBadRequest},
		{name: "infra failure: not found", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "999", wantStatus: http.StatusNotFound},
		{name: "hostile: unauthorized non-admin", claims: &UserClaims{ID: 62, IsAdmin: false, CanEditDeployments: true}, id: "1", seedProject: &db.GitProject{Name: "restricted"}, wantStatus: http.StatusForbidden},
		{name: "happy path: admin triggers sync", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "1", seedProject: &db.GitProject{Name: "p1", Status: "synced"}, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedProject != nil {
				db.GormDB.Create(tc.seedProject)
			}
			if tc.claims.ID == 62 {
				db.GormDB.Save(&db.User{ID: 62, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"})
			}
			_, c, rec := newFormRequest(http.MethodPost, "/gitops/projects/"+tc.id+"/sync", nil)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockClaimsContext(c, tc.claims)
			assert.NoError(t, handlePOSTGitopsProjectsIdSync()(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

func TestHandleDELETEGitopsProjectsId_Table(t *testing.T) {
	tests := []struct {
		name        string
		claims      *UserClaims
		id          string
		seedProject *db.GitProject
		wantStatus  int
	}{
		{name: "hostile: non-admin lacks permission", claims: &UserClaims{ID: 1, IsAdmin: false}, id: "1", wantStatus: http.StatusForbidden},
		{name: "hostile: invalid id", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "../x", wantStatus: http.StatusBadRequest},
		{name: "infra failure: not found", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "999", wantStatus: http.StatusNotFound},
		{name: "hostile: unauthorized non-admin", claims: &UserClaims{ID: 63, IsAdmin: false, CanDeleteDeployments: true}, id: "1", seedProject: &db.GitProject{Name: "restricted"}, wantStatus: http.StatusForbidden},
		{name: "happy path: admin deletes project", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "1", seedProject: &db.GitProject{Name: "p1"}, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedProject != nil {
				db.GormDB.Create(tc.seedProject)
			}
			if tc.claims.ID == 63 {
				db.GormDB.Save(&db.User{ID: 63, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"})
			}
			_, c, rec := newFormRequest(http.MethodDelete, "/gitops/projects/"+tc.id, nil)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockClaimsContext(c, tc.claims)
			assert.NoError(t, handleDELETEGitopsProjectsId()(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}

func TestHandleGETGitopsProjectsIdDeployments_Table(t *testing.T) {
	tests := []struct {
		name        string
		claims      *UserClaims
		id          string
		seedProject *db.GitProject
		wantStatus  int
	}{
		{name: "hostile: invalid id", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "../x", wantStatus: http.StatusBadRequest},
		{name: "infra failure: not found", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "999", wantStatus: http.StatusNotFound},
		{name: "hostile: unauthorized non-admin", claims: &UserClaims{ID: 64, IsAdmin: false}, id: "1", seedProject: &db.GitProject{Name: "restricted"}, wantStatus: http.StatusForbidden},
		{name: "happy path: admin sees deployments", claims: &UserClaims{ID: 1, IsAdmin: true}, id: "1", seedProject: &db.GitProject{Name: "p1"}, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, db.InitDB(":memory:"))
			if tc.seedProject != nil {
				db.GormDB.Create(tc.seedProject)
				db.GormDB.Create(&db.GitDeployment{ProjectID: tc.seedProject.ID, Status: "success"})
			}
			if tc.claims.ID == 64 {
				db.GormDB.Save(&db.User{ID: 64, IsActive: true, IsRestrictedAccess: true, AllowedContainers: "nope"})
			}
			_, c, rec := newFormRequest(http.MethodGet, "/gitops/projects/"+tc.id+"/deployments", nil)
			c.SetParamNames("id")
			c.SetParamValues(tc.id)
			mockClaimsContext(c, tc.claims)
			assert.NoError(t, handleGETGitopsProjectsIdDeployments()(c))
			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus == http.StatusOK {
				assert.Contains(t, rec.Body.String(), "success")
			}
		})
	}
}
