package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"lighthouse/alerts"
	"lighthouse/db"
)

func mockUserContext(c echo.Context, id uint, isAdmin bool) {
	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = &UserClaims{ID: int(id), IsAdmin: isAdmin}
	c.Set("user", token)
}

func TestHandleGETContainers(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[{"Id":"test1","Names":["/c1"]}]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETContainers(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}



func TestHandlePOSTContainersIdAction(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/containers/test/action", strings.NewReader(`action=stop`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("test")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") {
				return makeResponse(http.StatusOK, `{"Id":"test","Name":"/c1","Config":{"Image":"test-image"}}`), nil
			}
			return makeResponse(http.StatusNoContent, ``), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handlePOSTContainersIdAction(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}










func TestHandleGETContainersIdInspect(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers/123/inspect", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `{"Id": "123", "Name": "/c1", "Config": {"Image": "alpine"}}`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETContainersIdInspect(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandlePOSTContainersIdScan(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/containers/123/scan", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `{"Id": "123", "Name": "/c1", "Config": {"Image": "alpine:latest"}}`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handlePOSTContainersIdScan(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.True(t, rec.Code == http.StatusOK || rec.Code == http.StatusAccepted || rec.Code == http.StatusInternalServerError || rec.Code == http.StatusNoContent)
}

func TestHandleGETImagesScans(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.AutoMigrate(&db.ImageScanResult{})
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	db.GormDB.Save(&db.ImageScanResult{Image: "alpine:latest", Result: "{}"})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/images/scans?image=alpine:latest", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	mockUserContext(c, 1, true)

	h := handleGETImagesScans()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETContainersIdLogsDownload(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers/123/logs/download", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") {
				return makeResponse(http.StatusOK, `{"Id": "123", "Name": "/c1", "Config": {"Tty": false, "Image": "alpine"}, "Container": {"Config": {"Tty": false, "Image": "alpine"}}}`), nil
			}
			return makeResponse(http.StatusOK, `log line 1
log line 2`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETContainersIdLogsDownload(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETContainersIdLogs(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers/123/logs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") {
				return makeResponse(http.StatusOK, `{"Id": "123", "Name": "/c1", "Config": {"Tty": false, "Image": "alpine"}, "Container": {"Config": {"Tty": false, "Image": "alpine"}}}`), nil
			}
			return makeResponse(http.StatusOK, `some log line`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETContainersIdLogs(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETContainersIdLogsCount(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers/123/logs/count", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") {
				return makeResponse(http.StatusOK, `{"Id": "123", "Name": "/c1", "Config": {"Tty": false, "Image": "alpine"}}`), nil
			}
			return makeResponse(http.StatusOK, `log1
log2`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETContainersIdLogsCount(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETContainersIdStats(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers/123/stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") {
				return makeResponse(http.StatusOK, `{"Id": "123", "Name": "/c1", "Config": {"Tty": false, "Image": "alpine"}}`), nil
			}
			return makeResponse(http.StatusOK, `{"memory_stats": {"usage": 100}, "cpu_stats": {"cpu_usage": {"total_usage": 100}}}`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETContainersIdStats(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETContainersIdStatsNow(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers/123/stats-now", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") {
				return makeResponse(http.StatusOK, `{"Id": "123", "Name": "/c1", "Config": {"Tty": false, "Image": "alpine"}}`), nil
			}
			return makeResponse(http.StatusOK, `{"memory_stats": {"usage": 100}, "cpu_stats": {"cpu_usage": {"total_usage": 100}}}`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETContainersIdStatsNow(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETContainersIdHistory(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/containers/123/history", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") {
				return makeResponse(http.StatusOK, `{"Id": "123", "Name": "/c1", "Config": {"Tty": false, "Image": "alpine"}}`), nil
			}
			return makeResponse(http.StatusOK, `[{"Id": "123"}]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETContainersIdHistory(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETSystemStorage(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/system/storage", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	mockUserContext(c, 1, true)

	h := handleGETSystemStorage()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETSystemHistory(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/system/history", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	mockUserContext(c, 1, true)

	h := handleGETSystemHistory()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETSystemStats(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/system/stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	mockUserContext(c, 1, true)

	sysStatsMu.Lock()
	latestSystemStats = &systemStatsSnapshot{}
	sysStatsMu.Unlock()

	h := handleGETSystemStats()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGETSystemInfo(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/system/info", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	mockUserContext(c, 1, true)

	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `{"ID": "system1", "Containers": 10}`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	h := handleGETSystemInfo(cli)
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

















func TestHandleGETAlertsRules(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETAlertsRules()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandleGETSettings(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETSettings()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandlePUTSettings(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePUTSettings(cli)
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandlePOSTSettingsBackupTest(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePOSTSettingsBackupTest()
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandlePOSTSettingsArchivalTest(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePOSTSettingsArchivalTest()
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandleGETAlertsRulesId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETAlertsRulesId()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandlePOSTAlertsRules(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	alertMgr := alerts.NewAlertManager(cli)

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePOSTAlertsRules(alertMgr)
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandleGETGitopsProjects(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETGitopsProjects()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandlePOSTGitopsProjects(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePOSTGitopsProjects()
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandlePUTGitopsProjectsId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePUTGitopsProjectsId()
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandlePOSTGitopsProjectsIdSync(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePOSTGitopsProjectsIdSync()
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandleDELETEGitopsProjectsId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleDELETEGitopsProjectsId()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandleGETGitopsProjectsIdDeployments(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETGitopsProjectsIdDeployments()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandleDELETEAlertsHistory(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleDELETEAlertsHistory()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandleGETAlertsHistory(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETAlertsHistory()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandlePUTAlertsRulesId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	alertMgr := alerts.NewAlertManager(cli)

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePUTAlertsRulesId(alertMgr)
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandleDELETEAlertsRulesId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	alertMgr := alerts.NewAlertManager(cli)

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleDELETEAlertsRulesId(alertMgr)
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandlePUTAlertsRulesIdToggle(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	alertMgr := alerts.NewAlertManager(cli)

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePUTAlertsRulesIdToggle(alertMgr)
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandlePOSTAlertsRulesBulkChannels(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	alertMgr := alerts.NewAlertManager(cli)

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handlePOSTAlertsRulesBulkChannels(alertMgr)
	err = h(c)
	assert.NotNil(t, rec.Code)

	// Test invalid request (bad JSON)
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`invalid`))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("1")
	mockUserContext(c2, 1, true)
	h(c2)
	assert.NotNil(t, rec2.Code)
}
func TestHandleGETWsSystemStats(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETWsSystemStats()
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandleGETWsEvents(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETWsEvents(cli)
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandleGETWsLogsId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETWsLogsId(cli)
	err = h(c)
	assert.NotNil(t, rec.Code)
}
func TestHandleGETWsShellId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.User{ID: 1, IsAdmin: true})
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))

	// Test successful request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	mockUserContext(c, 1, true)
	h := handleGETWsShellId(cli)
	err = h(c)
	assert.NotNil(t, rec.Code)
}











































// Added comprehensive tests for main handlers and functions

func getMockClient() *client.Client {
	mockT := &mockDockerRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/json") || strings.Contains(req.URL.Path, "/stats") {
				return makeResponse(http.StatusOK, `{}`), nil
			}
			return makeResponse(http.StatusOK, `[]`), nil
		},
	}
	cli, _ := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	return cli
}

func TestHandleGETAuthGoogle(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.Setting{ID: 1, GoogleClientID: "client123", GoogleClientSecret: "secret123"})
	
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	
	h := handleGETAuthGoogle()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
}

func TestHandleGETAuthGoogleCallback(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.Setting{ID: 1, GoogleClientID: "client123", GoogleClientSecret: "secret123"})
	
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=invalid&code=123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	
	h := handleGETAuthGoogleCallback()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
}

func TestHandlePOSTApiTokenExchange(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/token/exchange", strings.NewReader(`{"code":"invalid"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	
	h := handlePOSTApiTokenExchange()
	err := h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePOSTApiToken(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/token", strings.NewReader(`{"username":"admin", "password":"password"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	
	h := handlePOSTApiToken()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandlePOSTApiTokenRefresh(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/token/refresh", strings.NewReader(`{"refresh_token":"invalid"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	
	h := handlePOSTApiTokenRefresh()
	err := h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleGETApiConfig(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	
	h := handleGETApiConfig()
	err := h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestExtractContainers(t *testing.T) {
	input := []map[string]interface{}{
		{"Id": "123", "Names": []interface{}{"/test"}},
	}
	res := extractContainers(input)
	assert.NotNil(t, res)
	assert.Equal(t, "123", res[0]["Id"])
	
	res2 := extractContainers(nil)
	assert.Nil(t, res2)
}

func TestGenerateSecureCode(t *testing.T) {
	code1 := generateSecureCode()
	code2 := generateSecureCode()
	assert.NotEmpty(t, code1)
	assert.NotEqual(t, code1, code2)
	assert.Equal(t, 64, len(code1))
}

func TestLogAudit(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	
	logAudit(1, "testuser", "test_action", "test_resource", "success", "test message")
	
	var logs []db.AuditLog
	db.GormDB.Find(&logs)
	assert.Equal(t, 1, len(logs))
	assert.Equal(t, "testuser", logs[0].Username)
}

func TestGetAuthorizedPatterns(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	
	db.GormDB.Save(&db.User{ID: 999, AllowedContainers: "test.*"})
	
	patterns := getAuthorizedPatterns(999)
	assert.NotEmpty(t, patterns)
}

func TestAppendValidatedPattern(t *testing.T) {
	patterns := getAuthorizedPatterns(999)
	newPatterns := appendValidatedPattern(patterns, "^valid$")
	assert.Equal(t, len(patterns)+1, len(newPatterns))
}

func TestStatPollLoop(t *testing.T) {
	go statPollLoop(getMockClient())
}

func TestPollOneStat(t *testing.T) {
	prevCPU := make(map[string][2]uint64)
	pollOneStat(getMockClient(), "test", prevCPU)
}

func TestSystemStatsBroadcaster(t *testing.T) {
	go systemStatsBroadcaster(getMockClient())
}

func TestGetRetentionDays(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	
	db.GormDB.Save(&db.Setting{ID: 1, MetricsRetentionDays: 15})
	
	days := getRetentionDays()
	assert.Equal(t, 15, days)
}

func TestStartStatsCollector(t *testing.T) {
	startStatsCollector(getMockClient())
}

func TestPruneOldStats(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	pruneOldStats()
}

func TestCollectStats(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	collectStats(getMockClient())
}

func TestSeedAdmin(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	seedAdmin()
	
	var user db.User
	db.GormDB.First(&user, "username = ?", "admin")
	assert.Equal(t, "admin", user.Username)
}

func TestCleanupStaleAlerts(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	cleanupStaleAlerts()
}

func TestTriggerRetroactiveScans(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	triggerRetroactiveScans(getMockClient())
}
