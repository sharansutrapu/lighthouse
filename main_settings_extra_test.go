package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func newSettingsClient(t *testing.T) *client.Client {
	mockT := &mockDockerRoundTripper{handler: func(req *http.Request) (*http.Response, error) {
		return makeResponse(http.StatusOK, `[]`), nil
	}}
	cli, err := client.NewClientWithOpts(client.WithHTTPClient(&http.Client{Transport: mockT}), client.WithVersion("1.41"))
	assert.NoError(t, err)
	return cli
}

func TestHandlePUTSettings_Table(t *testing.T) {
	cli := newSettingsClient(t)

	t.Run("hostile: invalid JSON body", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		e := echo.New()
		req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader("not-json"))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		mockUserContext(c, 1, true)
		assert.NoError(t, handlePUTSettings(cli)(c))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("happy path: masked secrets are preserved, not overwritten", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Updates(&db.Setting{
			SmtpPass: "realSmtpSecret", GoogleClientSecret: "realGoogleSecret",
			BackupAuth1: "realBackupAuth1", BackupAuth2: "realBackupSecret",
			ArchivalAuth1: "realArchivalAuth1", ArchivalAuth2: "realArchivalSecret",
			SlackWebhookUrl: "https://hooks.slack.com/real", MSTeamsWebhookUrl: "https://real.webhook.office.com",
			GChatWebhookUrl: "https://chat.googleapis.com/real", GenericWebhookUrl: "https://api.real.com/hook",
		})

		body := `{"smtp_pass":"********","google_client_secret":"********","backup_auth1":"********","backup_auth2":"********","archival_auth1":"********","archival_auth2":"********","slack_webhook_url":"********","msteams_webhook_url":"********","gchat_webhook_url":"********","generic_webhook_url":"********"}`
		e := echo.New()
		req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		mockUserContext(c, 1, true)
		assert.NoError(t, handlePUTSettings(cli)(c))
		assert.Equal(t, http.StatusOK, rec.Code)

		var s db.Setting
		assert.NoError(t, db.GormDB.First(&s, 1).Error)
		assert.Equal(t, "realSmtpSecret", s.SmtpPass)
		assert.Equal(t, "realGoogleSecret", s.GoogleClientSecret)
		assert.Equal(t, "realBackupAuth1", s.BackupAuth1)
		assert.Equal(t, "realBackupSecret", s.BackupAuth2)
		assert.Equal(t, "realArchivalAuth1", s.ArchivalAuth1)
		assert.Equal(t, "realArchivalSecret", s.ArchivalAuth2)
		assert.Equal(t, "https://hooks.slack.com/real", s.SlackWebhookUrl)
		assert.Equal(t, "https://real.webhook.office.com", s.MSTeamsWebhookUrl)
		assert.Equal(t, "https://chat.googleapis.com/real", s.GChatWebhookUrl)
		assert.Equal(t, "https://api.real.com/hook", s.GenericWebhookUrl)
	})

	t.Run("happy path: GET response masks webhook URLs and backup/archival auth1", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Updates(&db.Setting{
			BackupAuth1: `{"type":"service_account"}`, ArchivalAuth1: "AKIAEXAMPLE",
			SlackWebhookUrl: "https://hooks.slack.com/real", MSTeamsWebhookUrl: "https://real.webhook.office.com",
			GChatWebhookUrl: "https://chat.googleapis.com/real", GenericWebhookUrl: "https://api.real.com/hook",
		})

		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/settings", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		mockUserContext(c, 1, true)
		assert.NoError(t, handleGETSettings()(c))
		assert.Equal(t, http.StatusOK, rec.Code)

		body := rec.Body.String()
		assert.NotContains(t, body, "service_account")
		assert.NotContains(t, body, "AKIAEXAMPLE")
		assert.NotContains(t, body, "hooks.slack.com/real")
		assert.NotContains(t, body, "real.webhook.office.com")
		assert.NotContains(t, body, "chat.googleapis.com/real")
		assert.NotContains(t, body, "api.real.com/hook")
	})

	t.Run("happy path: AutoScanEnabled transitioning false->true triggers a retroactive sweep", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		db.GormDB.Model(&db.Setting{}).Where("id = ?", 1).Update("auto_scan_enabled", false)

		body := `{"auto_scan_enabled":true}`
		e := echo.New()
		req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		mockUserContext(c, 1, true)
		assert.NoError(t, handlePUTSettings(cli)(c))
		assert.Equal(t, http.StatusOK, rec.Code)

		var s db.Setting
		assert.NoError(t, db.GormDB.First(&s, 1).Error)
		assert.True(t, s.AutoScanEnabled)
	})
}
