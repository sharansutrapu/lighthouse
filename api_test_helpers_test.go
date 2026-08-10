package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
)

type mockDockerRoundTripper struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (m *mockDockerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req)
}

func mockDockerClientWithRoundTripper(t *testing.T, handler func(req *http.Request) (*http.Response, error)) *client.Client {
	cli, err := client.NewClientWithOpts(
		client.WithHTTPClient(&http.Client{
			Transport: &mockDockerRoundTripper{handler: handler},
		}),
		// Prevent ping on init
		client.WithVersion("1.41"),
	)
	if err != nil {
		t.Fatalf("Failed to create mock docker client: %v", err)
	}
	return cli
}

func makeResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}

func setupEchoWithClaimsHelper(claims *UserClaims) (*echo.Echo, *echo.Group, echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// inject token
	var token *jwt.Token
	if claims != nil {
		token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		c.Set("user", token)
	}

	g := e.Group("/api")
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if token != nil {
				ctx.Set("user", token)
			}
			return next(ctx)
		}
	})

	return e, g, c, rec
}
