package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
	"lighthouse/db"
)

func TestHandlePOSTUserChangePassword(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)

	hashed, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.DefaultCost)
	user := db.User{Username: "testuser", Password: string(hashed), IsActive: true, PasswordChanged: true}
	db.GormDB.Save(&user)

	e := echo.New()
	f := make(url.Values)
	f.Set("current_password", "oldpassword")
	f.Set("password", "newlongpassword")
	
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(f.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{
		ID: int(user.ID), Username: user.Username, RegisteredClaims: jwt.RegisteredClaims{},
	})
	c.Set("user", token)

	h := handlePOSTUserChangePassword()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var updatedUser db.User
	db.GormDB.First(&updatedUser, user.ID)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(updatedUser.Password), []byte("newlongpassword")))
}

func TestHandleGETUserMe(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	user := db.User{Username: "meuser", IsActive: true, IsAdmin: true}
	db.GormDB.Save(&user)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{
		ID: int(user.ID), Username: user.Username, RegisteredClaims: jwt.RegisteredClaims{},
	})
	c.Set("user", token)

	h := handleGETUserMe()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var res map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &res)
	assert.NoError(t, err)
	assert.Equal(t, "meuser", res["username"])
	assert.Equal(t, true, res["is_admin"])
}

func TestHandleGETUsers(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	db.GormDB.Create(&db.User{Username: "user1", Email: "user1@example.com", IsActive: true})
	db.GormDB.Create(&db.User{Username: "user2", Email: "user2@example.com", IsActive: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := handleGETUsers()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var res map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &res)
	assert.NoError(t, err)
	users := res["users"].([]interface{})
	assert.Len(t, users, 2)
}

func TestHandlePUTUsersIdActive(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	user := db.User{Username: "toggleuser", IsActive: false}
	db.GormDB.Save(&user)

	e := echo.New()
	f := make(url.Values)
	f.Set("is_active", "true")
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(f.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1") // assumes id=1
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{
		ID: 999, Username: "admin", RegisteredClaims: jwt.RegisteredClaims{},
	})
	c.Set("user", token)

	h := handlePUTUsersIdActive()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var updated db.User
	db.GormDB.First(&updated, user.ID)
	assert.True(t, updated.IsActive)
}

func TestHandlePOSTUsers(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	rt := db.RoleTemplate{Name: "Template", CanStart: true}
	db.GormDB.Save(&rt)

	e := echo.New()
	f := make(url.Values)
	f.Set("authMethod", "local")
	f.Set("role_template_id", "1")
	f.Set("username", "newuser")
	f.Set("password", "newlongpassword")
	
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(f.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{
		ID: 999, Username: "admin", RegisteredClaims: jwt.RegisteredClaims{},
	})
	c.Set("user", token)

	h := handlePOSTUsers()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var u db.User
	db.GormDB.Where("username = ?", "newuser").First(&u)
	assert.Equal(t, "newuser", u.Username)
	assert.True(t, u.CanStart)
}

func TestHandlePUTUsersIdPermissions(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	user := db.User{Username: "permuser", Email: "permuser@example.com"}
	db.GormDB.Create(&user)

	// Set global permission flags so clampStaffActionPermissions passes through
	origCanStart, origCanStop := CanStart, CanStop
	CanStart, CanStop = true, true
	defer func() { CanStart, CanStop = origCanStart, origCanStop }()

	e := echo.New()
	f := make(url.Values)
	f.Set("can_start", "true")
	f.Set("can_stop", "true")
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(f.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(strconv.Itoa(int(user.ID)))

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{
		ID: 999, Username: "admin", RegisteredClaims: jwt.RegisteredClaims{},
	})
	c.Set("user", token)

	h := handlePUTUsersIdPermissions()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var u db.User
	db.GormDB.First(&u, user.ID)
	assert.True(t, u.CanStart)
	assert.True(t, u.CanStop)
}

func TestHandlePUTUsersIdPassword(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	user := db.User{Username: "passuser"}
	db.GormDB.Save(&user)

	e := echo.New()
	f := make(url.Values)
	f.Set("password", "anewlongpassword")
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(f.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{
		ID: 999, Username: "admin", RegisteredClaims: jwt.RegisteredClaims{},
	})
	c.Set("user", token)

	h := handlePUTUsersIdPassword()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var u db.User
	db.GormDB.First(&u, user.ID)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.Password), []byte("anewlongpassword")))
	assert.True(t, u.PasswordChanged)
}

func TestHandleDELETEUsersId(t *testing.T) {
	err := db.InitDB(":memory:")
	assert.NoError(t, err)
	user := db.User{Username: "deleteuser", Email: "deleteuser@example.com"}
	db.GormDB.Save(&user)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Make sure ID is not 1 because ID 1 is protected
	c.SetParamNames("id")
	c.SetParamValues("2")
	user2 := db.User{ID: 2, Username: "deleteuser2", Email: "deleteuser2@example.com"}
	db.GormDB.Save(&user2)
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{
		ID: 999, Username: "admin", RegisteredClaims: jwt.RegisteredClaims{},
	})
	c.Set("user", token)

	h := handleDELETEUsersId()
	err = h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var count int64
	db.GormDB.Model(&db.User{}).Where("id = ?", 2).Count(&count)
	assert.Equal(t, int64(0), count)
}
