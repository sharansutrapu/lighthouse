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

func TestNetworksHandlers_ErrorAndPruneBranches(t *testing.T) {
	errHandler := func(req *http.Request) (*http.Response, error) {
		return makeResponse(http.StatusInternalServerError, `{"message":"docker daemon error"}`), nil
	}
	claims := &UserClaims{ID: 1, Username: "admin", IsAdmin: true, CanDelete: true}

	t.Run("infra failure: GET /networks propagates docker error as 500", func(t *testing.T) {
		cli := mockDockerClientWithRoundTripper(t, errHandler)
		e, g, _, _ := setupEchoWithClaimsHelper(claims)
		RegisterNetworkRoutes(g, cli)
		req := httptest.NewRequest(http.MethodGet, "/api/networks", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("infra failure: DELETE /networks/:id propagates docker error as 500", func(t *testing.T) {
		cli := mockDockerClientWithRoundTripper(t, errHandler)
		e, g, _, _ := setupEchoWithClaimsHelper(claims)
		RegisterNetworkRoutes(g, cli)
		req := httptest.NewRequest(http.MethodDelete, "/api/networks/test-net", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("happy path: prune with remove_containers=true prunes containers first", func(t *testing.T) {
		cli := mockDockerClientWithRoundTripper(t, mockHandlerForNetworks)
		e, g, _, _ := setupEchoWithClaimsHelper(claims)
		RegisterNetworkRoutes(g, cli)
		req := httptest.NewRequest(http.MethodPost, "/api/networks/prune", strings.NewReader(`{"remove_containers": true}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("edge case: prune with stopped containers present sets a warning", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if req.Method == "GET" && strings.Contains(path, "/containers/json") {
				return makeResponse(http.StatusOK, `[{"Id": "stopped-container"}]`), nil
			}
			return mockHandlerForNetworks(req)
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		e, g, _, _ := setupEchoWithClaimsHelper(claims)
		RegisterNetworkRoutes(g, cli)
		req := httptest.NewRequest(http.MethodPost, "/api/networks/prune", strings.NewReader(`{"remove_containers": false}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Stopped containers detected")
	})

	t.Run("infra failure: NetworkPrune error returns 500", func(t *testing.T) {
		handler := func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if req.Method == "POST" && strings.HasSuffix(path, "/networks/prune") {
				return makeResponse(http.StatusInternalServerError, `{"message":"prune failed"}`), nil
			}
			return mockHandlerForNetworks(req)
		}
		cli := mockDockerClientWithRoundTripper(t, handler)
		e, g, _, _ := setupEchoWithClaimsHelper(claims)
		RegisterNetworkRoutes(g, cli)
		req := httptest.NewRequest(http.MethodPost, "/api/networks/prune", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}


