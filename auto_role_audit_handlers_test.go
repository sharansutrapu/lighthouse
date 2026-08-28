package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"lighthouse/db"
)

func TestHandleGETAudit(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.AuditLog{Username: "admin", Action: "LOGIN", Timestamp: time.Now()})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := handleGETAudit()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var logs []map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &logs)
	assert.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.Equal(t, "admin", logs[0]["username"])
}

func TestHandleGETAudit_FromToFilter(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.AuditLog{Username: "admin", Action: "LOGIN", Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})
	db.GormDB.Save(&db.AuditLog{Username: "admin", Action: "LOGOUT", Timestamp: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/audit?from=2024-05-01&to=2024-07-01", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := handleGETAudit()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var logs []map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &logs)
	assert.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.Equal(t, "LOGOUT", logs[0]["action"])
}

func TestHandleGETRoleTemplates(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Save(&db.RoleTemplate{Name: "Template A"})
	db.GormDB.Save(&db.RoleTemplate{Name: "Template B"})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/roles", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := handleGETRoleTemplates()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var roles []map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &roles)
	assert.NoError(t, err)
	assert.Len(t, roles, 4)
}

func TestHandlePOSTRoleTemplates(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)

	e := echo.New()
	reqBody := `{"name":"New Role","can_start":true}`
	req := httptest.NewRequest(http.MethodPost, "/roles", bytes.NewBufferString(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := handlePOSTRoleTemplates()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var role db.RoleTemplate
	db.GormDB.Where("name = ?", "New Role").First(&role)
	assert.Equal(t, "New Role", role.Name)
	assert.True(t, role.CanStart)
	assert.Equal(t, ".*", role.AllowedContainers)

	// Test invalid payload
	req2 := httptest.NewRequest(http.MethodPost, "/roles", bytes.NewBufferString("invalid"))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	err = h(c2)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestHandleDELETERoleTemplatesId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	role := db.RoleTemplate{Name: "To Delete"}
	db.GormDB.Save(&role)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h := handleDELETERoleTemplatesId()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var count int64
	db.GormDB.Model(&db.RoleTemplate{}).Count(&count)
	assert.Equal(t, int64(2), count)

	// Test invalid ID format
	c.SetParamValues("invalid")
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues("invalid")
	err = h(c2)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
}
