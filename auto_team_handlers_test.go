package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func TestHandleGETTeams(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.Team{Name: "Team A", Description: "Desc A"})
	db.GormDB.Save(&db.Team{Name: "Team B", Description: "Desc B"})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/teams", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := handleGETTeams()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	
	var teams []db.Team
	err = json.Unmarshal(rec.Body.Bytes(), &teams)
	assert.NoError(t, err)
	assert.Len(t, teams, 2)
	assert.Equal(t, "Team A", teams[0].Name)
}

func TestHandlePOSTTeams(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)

	e := echo.New()
	f := make(url.Values)
	f.Set("name", "New Team")
	f.Set("description", "A description")
	f.Set("allowed_containers", ".*")
	f.Set("can_start", "true")

	req := httptest.NewRequest(http.MethodPost, "/teams", strings.NewReader(f.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := handlePOSTTeams()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var team db.Team
	db.GormDB.First(&team)
	assert.Equal(t, "New Team", team.Name)
	assert.True(t, team.CanStart)

	// Test missing name
	req2 := httptest.NewRequest(http.MethodPost, "/teams", strings.NewReader(""))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	err = h(c2)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestHandlePUTTeamsId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	team := db.Team{Name: "Old Name"}
	db.GormDB.Save(&team)

	e := echo.New()
	f := make(url.Values)
	f.Set("name", "Updated Name")
	f.Set("can_stop", "true")

	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(f.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h := handlePUTTeamsId()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var updatedTeam db.Team
	db.GormDB.First(&updatedTeam, team.ID)
	assert.Equal(t, "Updated Name", updatedTeam.Name)
	assert.True(t, updatedTeam.CanStop)

	// Test invalid ID
	c.SetParamValues("invalid")
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("invalid")
	err = h(c2)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestHandleDELETETeamsId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	team := db.Team{Name: "To Delete"}
	db.GormDB.Save(&team)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h := handleDELETETeamsId()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var count int64
	db.GormDB.Model(&db.Team{}).Count(&count)
	assert.Equal(t, int64(0), count)

	// Test invalid ID
	c.SetParamValues("invalid")
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("invalid")
	err = h(c2)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
}
