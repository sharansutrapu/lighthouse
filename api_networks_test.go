package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func mockHandlerForNetworks(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	if req.Method == "GET" && strings.HasSuffix(path, "/networks") {
		return makeResponse(http.StatusOK, `[{"Id": "test-net"}]`), nil
	} else if req.Method == "DELETE" && strings.Contains(path, "/networks/") {
		return makeResponse(http.StatusOK, `{}`), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/networks/prune") {
		return makeResponse(http.StatusOK, `{"NetworksDeleted": ["test-net"]}`), nil
	} else if req.Method == "GET" && strings.HasSuffix(path, "/_ping") {
		return makeResponse(http.StatusOK, `OK`), nil
	}
	return makeResponse(http.StatusNotFound, `{"message":"not found"}`), nil
}

func TestNetworksHandlers(t *testing.T) {
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForNetworks)
	claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}
	e, g, _, _ := setupEchoWithClaimsHelper(claims)
	RegisterNetworkRoutes(g, cli)

	// GET
	req := httptest.NewRequest(http.MethodGet, "/api/networks", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/networks/test-net", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// PRUNE
	req = httptest.NewRequest(http.MethodPost, "/api/networks/prune", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNetworksHandlers_Forbidden(t *testing.T) {
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForNetworks)
	claims := &UserClaims{ID: 2, Username: "user", IsAdmin: false, CanDelete: false}
	e, g, _, _ := setupEchoWithClaimsHelper(claims)
	RegisterNetworkRoutes(g, cli)

	// DELETE
	req := httptest.NewRequest(http.MethodDelete, "/api/networks/test-net", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// PRUNE
	req = httptest.NewRequest(http.MethodPost, "/api/networks/prune", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

