package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func TestHandlePOSTTeams_RoleTemplateAndDuplicateName(t *testing.T) {
	t.Run("happy path: valid role_template_id is attached", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		db.GormDB.Create(&db.RoleTemplate{ID: 5, Name: "Custom"})

		f := url.Values{"name": {"team-with-role"}, "role_template_id": {"5"}}
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/teams", strings.NewReader(f.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		assert.NoError(t, handlePOSTTeams()(c))
		assert.Equal(t, http.StatusCreated, rec.Code)

		var team db.Team
		assert.NoError(t, db.GormDB.Where("name = ?", "team-with-role").First(&team).Error)
		assert.NotNil(t, team.RoleTemplateID)
		assert.Equal(t, uint(5), *team.RoleTemplateID)
	})

	t.Run("hostile: non-numeric role_template_id is silently ignored", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		f := url.Values{"name": {"team-bad-role"}, "role_template_id": {"not-a-number"}}
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/teams", strings.NewReader(f.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		assert.NoError(t, handlePOSTTeams()(c))
		assert.Equal(t, http.StatusCreated, rec.Code)

		var team db.Team
		assert.NoError(t, db.GormDB.Where("name = ?", "team-bad-role").First(&team).Error)
		assert.Nil(t, team.RoleTemplateID)
	})

	t.Run("infra failure: duplicate team name fails create", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		db.GormDB.Create(&db.Team{Name: "dup-team"})

		f := url.Values{"name": {"dup-team"}}
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/teams", strings.NewReader(f.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		assert.NoError(t, handlePOSTTeams()(c))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestHandlePUTTeamsId_RoleTemplateBranches(t *testing.T) {
	t.Run("happy path: valid role_template_id is attached", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		team := db.Team{Name: "team1"}
		db.GormDB.Create(&team)
		db.GormDB.Create(&db.RoleTemplate{ID: 7, Name: "Custom2"})

		f := url.Values{"name": {"team1"}, "role_template_id": {"7"}}
		e := echo.New()
		req := httptest.NewRequest(http.MethodPut, "/teams/1", strings.NewReader(f.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("1")

		assert.NoError(t, handlePUTTeamsId()(c))
		assert.Equal(t, http.StatusOK, rec.Code)

		var updated db.Team
		assert.NoError(t, db.GormDB.First(&updated, team.ID).Error)
		assert.NotNil(t, updated.RoleTemplateID)
		assert.Equal(t, uint(7), *updated.RoleTemplateID)
	})

	t.Run("happy path: role_template_id=null clears the association", func(t *testing.T) {
		assert.NoError(t, db.InitDB(":memory:"))
		rtID := uint(1)
		team := db.Team{Name: "team2", RoleTemplateID: &rtID}
		db.GormDB.Create(&team)

		f := url.Values{"name": {"team2"}, "role_template_id": {"null"}}
		e := echo.New()
		req := httptest.NewRequest(http.MethodPut, "/teams/1", strings.NewReader(f.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("1")

		assert.NoError(t, handlePUTTeamsId()(c))
		assert.Equal(t, http.StatusOK, rec.Code)

		var updated db.Team
		assert.NoError(t, db.GormDB.First(&updated, team.ID).Error)
		assert.Nil(t, updated.RoleTemplateID)
	})
}

func TestHandleGETTeams_MasksWebhookURLs(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	db.GormDB.Create(&db.Team{
		Name: "team-with-hooks", SlackWebhookUrl: "https://hooks.slack.com/real",
		MSTeamsWebhookUrl: "https://real.webhook.office.com", GChatWebhookUrl: "https://chat.googleapis.com/real",
		GenericWebhookUrl: "https://api.real.com/hook",
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/teams", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	assert.NoError(t, handleGETTeams()(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.NotContains(t, body, "hooks.slack.com/real")
	assert.NotContains(t, body, "real.webhook.office.com")
	assert.NotContains(t, body, "chat.googleapis.com/real")
	assert.NotContains(t, body, "api.real.com/hook")
	assert.Contains(t, body, "********")
}

func TestHandlePUTTeamsId_MaskedWebhooksArePreserved(t *testing.T) {
	assert.NoError(t, db.InitDB(":memory:"))
	team := db.Team{
		Name: "team-webhooks", SlackWebhookUrl: "https://hooks.slack.com/real",
		MSTeamsWebhookUrl: "https://real.webhook.office.com", GChatWebhookUrl: "https://chat.googleapis.com/real",
		GenericWebhookUrl: "https://api.real.com/hook",
	}
	db.GormDB.Create(&team)

	f := url.Values{
		"name": {"team-webhooks"}, "slack_webhook_url": {"********"}, "msteams_webhook_url": {"********"},
		"gchat_webhook_url": {"********"}, "generic_webhook_url": {"********"},
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/teams/1", strings.NewReader(f.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	assert.NoError(t, handlePUTTeamsId()(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var updated db.Team
	assert.NoError(t, db.GormDB.First(&updated, team.ID).Error)
	assert.Equal(t, "https://hooks.slack.com/real", updated.SlackWebhookUrl)
	assert.Equal(t, "https://real.webhook.office.com", updated.MSTeamsWebhookUrl)
	assert.Equal(t, "https://chat.googleapis.com/real", updated.GChatWebhookUrl)
	assert.Equal(t, "https://api.real.com/hook", updated.GenericWebhookUrl)
}
