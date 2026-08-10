package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func mockHandlerForImages(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	if req.Method == "GET" && strings.HasSuffix(path, "/images/json") {
		return makeResponse(http.StatusOK, `[{"Id": "sha256:123456"}]`), nil
	} else if req.Method == "DELETE" && strings.Contains(path, "/images/") {
		return makeResponse(http.StatusOK, `[{"Deleted": "sha256:123456"}]`), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/images/prune") {
		return makeResponse(http.StatusOK, `{"ImagesDeleted": [{"Deleted": "sha256:123456"}], "SpaceReclaimed": 100}`), nil
	} else if req.Method == "GET" && strings.HasSuffix(path, "/_ping") {
		return makeResponse(http.StatusOK, `OK`), nil
	}
	return makeResponse(http.StatusNotFound, `{"message":"not found"}`), nil
}

func TestImagesHandlers(t *testing.T) {
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForImages)
	claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
	e, g, _, _ := setupEchoWithClaimsHelper(claims)
	RegisterImageRoutes(g, cli)

	// GET
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/images/sha256:123456", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// PRUNE
	req = httptest.NewRequest(http.MethodPost, "/api/images/prune", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestImagesHandlers_Forbidden(t *testing.T) {
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForImages)
	claims := &UserClaims{ID: 2, Username: "user", IsAdmin: false, CanDelete: false}
	e, g, _, _ := setupEchoWithClaimsHelper(claims)
	RegisterImageRoutes(g, cli)

	// DELETE
	req := httptest.NewRequest(http.MethodDelete, "/api/images/sha256:123456", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// PRUNE
	req = httptest.NewRequest(http.MethodPost, "/api/images/prune", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestImagesHandlers_ErrorsAndFlags(t *testing.T) {
	// 1. GET images error
	errHandler := func(req *http.Request) (*http.Response, error) {
		path := req.URL.Path
		if req.Method == "GET" && strings.HasSuffix(path, "/images/json") {
			return nil, assert.AnError
		}
		if req.Method == "DELETE" && strings.Contains(path, "/images/") {
			return nil, assert.AnError
		}
		if req.Method == "POST" && strings.HasSuffix(path, "/images/prune") {
			return nil, assert.AnError
		}
		if req.Method == "GET" && strings.HasSuffix(path, "/containers/json") { // Mock container list returning exited container
			return makeResponse(http.StatusOK, `[{"Id": "c1", "Status": "exited"}]`), nil
		}
		return makeResponse(http.StatusNotFound, `{"message":"not found"}`), nil
	}

	cli := mockDockerClientWithRoundTripper(t, errHandler)
	claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
	e, g, _, _ := setupEchoWithClaimsHelper(claims)
	RegisterImageRoutes(g, cli)

	// GET error
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// DELETE error
	req = httptest.NewRequest(http.MethodDelete, "/api/images/sha256:error", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// PRUNE error (with AllUnused = true and RemoveContainers = false)
	req = httptest.NewRequest(http.MethodPost, "/api/images/prune", strings.NewReader(`{"all_unused": true, "remove_containers": false}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestImagesHandlers_PruneRemoveContainers(t *testing.T) {
	mockH := func(req *http.Request) (*http.Response, error) {
		path := req.URL.Path
		if req.Method == "POST" && strings.HasSuffix(path, "/containers/prune") {
			return makeResponse(http.StatusOK, `{}`), nil
		}
		if req.Method == "POST" && strings.HasSuffix(path, "/images/prune") {
			return makeResponse(http.StatusOK, `{"ImagesDeleted": [{"Deleted": "sha256:123456"}], "SpaceReclaimed": 100}`), nil
		}
		return makeResponse(http.StatusNotFound, `{"message":"not found"}`), nil
	}
	cli := mockDockerClientWithRoundTripper(t, mockH)
	claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
	e, g, _, _ := setupEchoWithClaimsHelper(claims)
	RegisterImageRoutes(g, cli)

	req := httptest.NewRequest(http.MethodPost, "/api/images/prune", strings.NewReader(`{"all_unused": false, "remove_containers": true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

