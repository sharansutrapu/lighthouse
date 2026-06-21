package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetworkRoutes(t *testing.T) {
	cli, ts := mockDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /_ping":
			w.Header().Set("API-Version", "1.41")
			w.Write([]byte("OK"))
		case "GET /v1.41/networks":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"Id": "nw_123456", "Name": "bridge"}]`))
		case "DELETE /v1.41/networks/nw_123456":
			w.WriteHeader(http.StatusNoContent)
		case "POST /v1.41/networks/prune":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"NetworksDeleted": ["nw_123456"]}`))
		default:
			t.Logf("Unhandled mock route: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer ts.Close()

	claims := &UserClaims{
		ID:        1,
		Username:  "admin",
		IsAdmin:   true,
		CanDelete: true,
	}

	e, g, _, _ := setupEchoWithClaims(claims)
	RegisterNetworkRoutes(g, cli)

	// Test GET /api/networks
	req := httptest.NewRequest(http.MethodGet, "/api/networks", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "nw_123456")

	// Test DELETE /api/networks/:id
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/networks/nw_123456", nil)
	recDel := httptest.NewRecorder()
	e.ServeHTTP(recDel, reqDel)

	assert.Equal(t, http.StatusOK, recDel.Code)

	// Test POST /api/networks/prune
	reqPrune := httptest.NewRequest(http.MethodPost, "/api/networks/prune", nil)
	recPrune := httptest.NewRecorder()
	e.ServeHTTP(recPrune, reqPrune)

	assert.Equal(t, http.StatusOK, recPrune.Code)
}

func TestNetworkRoutes_Forbidden(t *testing.T) {
	cli, ts := mockDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("API-Version", "1.41")
		w.Write([]byte("OK"))
	})
	defer ts.Close()

	claims := &UserClaims{
		IsAdmin:   false,
		CanDelete: false,
	}

	e, g, _, _ := setupEchoWithClaims(claims)
	RegisterNetworkRoutes(g, cli)

	// Test DELETE /api/networks/:id
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/networks/nw_123456", nil)
	recDel := httptest.NewRecorder()
	e.ServeHTTP(recDel, reqDel)

	assert.Equal(t, http.StatusForbidden, recDel.Code)

	// Test POST /api/networks/prune
	reqPrune := httptest.NewRequest(http.MethodPost, "/api/networks/prune", nil)
	recPrune := httptest.NewRecorder()
	e.ServeHTTP(recPrune, reqPrune)

	assert.Equal(t, http.StatusForbidden, recPrune.Code)
}
