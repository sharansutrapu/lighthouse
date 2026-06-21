package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
)

func mockDockerClient(t *testing.T, handler http.HandlerFunc) (*client.Client, *httptest.Server) {
	ts := httptest.NewServer(handler)
	cli, err := client.NewClientWithOpts(
		client.WithHost(ts.URL),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("Failed to create mock docker client: %v", err)
	}
	return cli, ts
}

func setupEchoWithClaims(claims *UserClaims) (*echo.Echo, *echo.Group, echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// inject token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	c.Set("user", token)

	g := e.Group("/api")
	// Middleware to inject the user token context for all group routes
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			ctx.Set("user", token)
			return next(ctx)
		}
	})

	return e, g, c, rec
}

func TestImageRoutes(t *testing.T) {
	cli, ts := mockDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /_ping":
			w.Header().Set("API-Version", "1.41")
			w.Write([]byte("OK"))
		case "GET /v1.41/images/json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"Id": "sha256:123456"}]`))
		case "DELETE /v1.41/images/sha256:123456":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"Deleted": "sha256:123456"}]`))
		case "POST /v1.41/images/prune":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ImagesDeleted": [{"Deleted": "sha256:123456"}], "SpaceReclaimed": 100}`))
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
	RegisterImageRoutes(g, cli)

	// Test GET /api/images
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "sha256:123456")

	// Test DELETE /api/images/:id
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/images/sha256:123456", nil)
	recDel := httptest.NewRecorder()
	e.ServeHTTP(recDel, reqDel)

	assert.Equal(t, http.StatusOK, recDel.Code)

	// Test POST /api/images/prune
	reqPrune := httptest.NewRequest(http.MethodPost, "/api/images/prune", nil)
	recPrune := httptest.NewRecorder()
	e.ServeHTTP(recPrune, reqPrune)

	assert.Equal(t, http.StatusOK, recPrune.Code)
}

func TestImageRoutes_Forbidden(t *testing.T) {
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
	RegisterImageRoutes(g, cli)

	// Test DELETE /api/images/:id
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/images/sha256:123456", nil)
	recDel := httptest.NewRecorder()
	e.ServeHTTP(recDel, reqDel)

	assert.Equal(t, http.StatusForbidden, recDel.Code)

	// Test POST /api/images/prune
	reqPrune := httptest.NewRequest(http.MethodPost, "/api/images/prune", nil)
	recPrune := httptest.NewRecorder()
	e.ServeHTTP(recPrune, reqPrune)

	assert.Equal(t, http.StatusForbidden, recPrune.Code)
}
