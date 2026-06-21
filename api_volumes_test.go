package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVolumeRoutes(t *testing.T) {
	cli, ts := mockDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /_ping":
			w.Header().Set("API-Version", "1.41")
			w.Write([]byte("OK"))
		case "GET /v1.41/volumes":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"Volumes": [{"Name": "vol_123456", "Driver": "local"}], "Warnings": []}`))
		case "DELETE /v1.41/volumes/vol_123456":
			w.WriteHeader(http.StatusNoContent)
		case "POST /v1.41/volumes/prune":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"VolumesDeleted": ["vol_123456"], "SpaceReclaimed": 500}`))
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
	RegisterVolumeRoutes(g, cli)

	// Test GET /api/volumes
	req := httptest.NewRequest(http.MethodGet, "/api/volumes", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "vol_123456")

	// Test DELETE /api/volumes/:id
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/volumes/vol_123456", nil)
	recDel := httptest.NewRecorder()
	e.ServeHTTP(recDel, reqDel)

	assert.Equal(t, http.StatusOK, recDel.Code)

	// Test POST /api/volumes/prune
	reqPrune := httptest.NewRequest(http.MethodPost, "/api/volumes/prune", nil)
	recPrune := httptest.NewRecorder()
	e.ServeHTTP(recPrune, reqPrune)

	assert.Equal(t, http.StatusOK, recPrune.Code)
}

func TestVolumeRoutes_Forbidden(t *testing.T) {
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
	RegisterVolumeRoutes(g, cli)

	// Test DELETE /api/volumes/:id
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/volumes/vol_123456", nil)
	recDel := httptest.NewRecorder()
	e.ServeHTTP(recDel, reqDel)

	assert.Equal(t, http.StatusForbidden, recDel.Code)

	// Test POST /api/volumes/prune
	reqPrune := httptest.NewRequest(http.MethodPost, "/api/volumes/prune", nil)
	recPrune := httptest.NewRecorder()
	e.ServeHTTP(recPrune, reqPrune)

	assert.Equal(t, http.StatusForbidden, recPrune.Code)
}
