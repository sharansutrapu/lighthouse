package main

import (
	"bytes"
	"encoding/json"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	"lighthouse/db"
)

func TestMain(m *testing.M) {
	if err := db.InitDB(":memory:"); err != nil {
		panic("failed to init test db: " + err.Error())
	}
	os.Exit(m.Run())
}

func TestConfigRoute(t *testing.T) {
	e := echo.New()

	// Temporarily override global config values for the test
	originalCanStart := CanStart
	originalAllowShell := AllowShell
	CanStart = true
	AllowShell = false
	defer func() {
		CanStart = originalCanStart
		AllowShell = originalAllowShell
	}()

	e.GET("/api/config", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"allow_start":   CanStart,
			"allow_stop":    CanStop,
			"allow_restart": CanRestart,
			"allow_delete":  CanDelete,
			"allow_shell":   AllowShell,
			"client_access": clientAccessConfig(),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, true, response["allow_start"])
	assert.Equal(t, false, response["allow_shell"])
}



func TestGeneratedHandlers(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	c.SetParamNames("id")
	c.SetParamValues("123")

	claims := &UserClaims{ID: 1, IsAdmin: true}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	c.Set("user", token)

	cli := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`[{}]`)), Header: make(http.Header)}, nil
	})

	handleGETAuthGoogle()(c)
	handleGETAuthGoogleCallback()(c)
	handlePOSTApiTokenExchange()(c)
	handlePOSTApiToken()(c)
	handlePOSTApiTokenRefresh()(c)
	handleGETApiConfig()(c)
	handleGETContainers(cli)(c)
	handleGETContainersIdInspect(cli)(c)
	handlePOSTContainersIdAction(cli)(c)
	handlePOSTContainersIdScan(cli)(c)
	handleGETImagesScans()(c)
	handleGETContainersIdLogsDownload(cli)(c)
	handleGETContainersIdLogs(cli)(c)
	handleGETContainersIdLogsCount(cli)(c)
	handleGETContainersIdStats(cli)(c)
	handleGETContainersIdStatsNow(cli)(c)
	handleGETContainersIdHistory(cli)(c)
	handleGETSystemStorage()(c)
	handleGETSystemHistory()(c)
	handleGETSystemStats()(c)
	handleGETSystemInfo(cli)(c)
	handlePOSTUserChangePassword()(c)
	handleGETUserMe()(c)
	handleGETTeams()(c)
	handlePOSTTeams()(c)
	handlePUTTeamsId()(c)
	handleDELETETeamsId()(c)
	handleGETUsers()(c)
	handlePUTUsersIdActive()(c)
	handlePOSTUsers()(c)
	handlePUTUsersIdPermissions()(c)
	handlePUTUsersIdPassword()(c)
	handleDELETEUsersId()(c)
	handleGETAudit()(c)
	handleGETRoleTemplates()(c)
	handlePOSTRoleTemplates()(c)
	handleDELETERoleTemplatesId()(c)
	handleGETAlertsRules()(c)
	handleGETSettings()(c)
	handlePUTSettings(cli)(c)
	handlePOSTSettingsBackupTest()(c)
	handlePOSTSettingsArchivalTest()(c)
	handleGETAlertsRulesId()(c)
	handleGETGitopsProjects()(c)
	handlePOSTGitopsProjects()(c)
	handlePUTGitopsProjectsId()(c)
	handlePOSTGitopsProjectsIdSync()(c)
	handleDELETEGitopsProjectsId()(c)
	handleGETGitopsProjectsIdDeployments()(c)
	handleDELETEAlertsHistory()(c)
	handleGETAlertsHistory()(c)
}
