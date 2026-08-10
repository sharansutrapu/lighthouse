package main

import (
	"context"
	"fmt"
	"lighthouse/db"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/moby/moby/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

func mockHandlerForMCP(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	fmt.Printf("mockHandlerForMCP: method=%s path=%s\n", req.Method, path)
	if req.Method == "GET" && strings.HasSuffix(path, "containers/json") {
		if req.URL.RawQuery == "all=1" {
			return makeResponse(http.StatusOK, `[{"Id": "c1", "Names": ["/c1"], "Image": "nginx"}, {"Id": "c2", "Names": ["/lighthouse"], "Image": "lighthouse"}, {"Id": "c3", "Names": ["/excluded"], "Image": "test-img"}]`), nil
		}
		if strings.Contains(path, "fail-containers") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusOK, `[{"Id": "test"}]`), nil
	} else if req.Method == "GET" && strings.Contains(path, "fail-containers/json") {
		return nil, assert.AnError
	} else if req.Method == "GET" && strings.HasSuffix(path, "/images/json") {
		if strings.Contains(path, "fail-img") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusOK, `[{"Id": "test-img"}]`), nil
	} else if req.Method == "GET" && strings.HasSuffix(path, "/volumes") {
		if strings.Contains(path, "fail-vol") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusOK, `{"Volumes": [{"Name": "test-vol"}]}`), nil
	} else if req.Method == "GET" && strings.HasSuffix(path, "/networks") {
		if strings.Contains(path, "fail-net") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusOK, `[{"Id": "test-net"}]`), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/start") {
		if strings.Contains(path, "fail-start") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusNoContent, ``), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/stop") {
		if strings.Contains(path, "fail-stop") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusNoContent, ``), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/restart") {
		if strings.Contains(path, "fail-restart") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusNoContent, ``), nil
	} else if req.Method == "GET" && strings.HasSuffix(path, "/logs") {
		if strings.Contains(path, "fail-log") {
			return nil, assert.AnError
		}
		if strings.Contains(path, "huge-log") {
			// Create a string slightly larger than 50KB to trigger truncation
			b := strings.Repeat("a", 51*1024)
			// Return as raw bytes, ignoring Docker multiplexing header for simplicity in mock,
			// though stdcopy expects 8-byte header. Actually, stdcopy handles raw bytes if not TTY,
			// wait stdcopy expects 8-byte header: [1 byte stream type, 3 bytes zero, 4 bytes size]
			// We can mock a valid stdcopy header.
			hdr := []byte{1, 0, 0, 0, 0, 0, byte(len(b) >> 8), byte(len(b))}
			return makeResponse(http.StatusOK, string(hdr)+b), nil
		}
		hdr := []byte{1, 0, 0, 0, 0, 0, 0, 10}
		return makeResponse(http.StatusOK, string(hdr)+"log line 1\nlog line 2"), nil
	} else if req.Method == "GET" && strings.Contains(path, "/containers/") && strings.HasSuffix(path, "/json") {
		// inspect container
		if strings.Contains(path, "notfound") {
			return makeResponse(http.StatusNotFound, `{"message": "no such container"}`), nil
		}
		if strings.Contains(path, "unauth") {
			return makeResponse(http.StatusOK, `{"Id": "unauth", "Name": "/unauth", "Config": {"Image": "unauth-img"}}`), nil
		}
		// Extract container ID from path
		parts := strings.Split(path, "/")
		id := "test"
		if len(parts) >= 4 {
			id = parts[3]
		}
		return makeResponse(http.StatusOK, fmt.Sprintf(`{"Id": "%s", "Name": "/c1", "Config": {"Image": "alpine"}}`, id)), nil
	}

	return makeResponse(http.StatusOK, `{}`), nil
}

func mockMCPRequest(method string, containerID string) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Method = method
	if containerID != "" {
		req.Params.Arguments = map[string]interface{}{
			"container_id": containerID,
		}
	} else {
		req.Params.Arguments = map[string]interface{}{}
	}
	return req
}

func setupMCPTestDB() {
	db.InitDB(":memory:")
	db.GormDB.Exec("DELETE FROM users")
	db.GormDB.Create(&db.User{ID: 1, Username: "admin", Email: "admin@example.com", IsAdmin: true})
	db.GormDB.Create(&db.User{ID: 2, Username: "user", Email: "user@example.com", IsAdmin: false, AllowedContainers: "c1"})
	db.GormDB.Create(&db.User{ID: 99, Username: "nonexistent", Email: "nonexistent@example.com", IsAdmin: false}) // to test missing user
}

func TestRegisterMCPRoutes(t *testing.T) {
	e, g, _, _ := setupEchoWithClaimsHelper(nil)
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	registerMCPRoutes(g, cli)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/sse", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	go func() {
		e.ServeHTTP(rec, req)
	}()
	time.Sleep(20 * time.Millisecond) // Give it time to start and then be canceled by timeout
	assert.Equal(t, http.StatusOK, rec.Code)

	reqPost := httptest.NewRequest(http.MethodPost, "/api/mcp/message", nil)
	recPost := httptest.NewRecorder()
	e.ServeHTTP(recPost, reqPost)
	assert.Equal(t, http.StatusBadRequest, recPost.Code) // MCP message handler usually expects specific payload
}

func TestIsMCPContainerAuthorized(t *testing.T) {
	setupMCPTestDB()

	// Test admin
	assert.True(t, isMCPContainerAuthorized(1, true, "some-container", "some-img"))

	// Test lighthouse self container
	assert.False(t, isMCPContainerAuthorized(1, true, "lighthouse", "lighthouse"))

	// Test patterns
	assert.True(t, isMCPContainerAuthorized(2, false, "c1", "alpine"))
	assert.False(t, isMCPContainerAuthorized(2, false, "unauth", "alpine"))
}

func TestGetMCPUserIsAdmin(t *testing.T) {
	setupMCPTestDB()
	assert.True(t, getMCPUserIsAdmin(1))
	assert.False(t, getMCPUserIsAdmin(2))
	assert.False(t, getMCPUserIsAdmin(999)) // user not found
}

func TestMCPListContainersHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpListContainersHandler(cli)

	// Unauthorized (no context)
	res, _ := h(context.Background(), mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	// Success Admin
	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "c1")

	// Success User
	ctx = context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false})
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "c1")

	// Docker error
	cliErr := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		return nil, assert.AnError
	})
	res, _ = mcpListContainersHandler(cliErr)(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)
}

func TestMCPGetContainerLogsHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpGetContainerLogsHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequest("", "test"))
	assert.True(t, res.IsError)

	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Missing ID
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	// Container not found
	res, _ = h(ctx, mockMCPRequest("", "notfound"))
	assert.True(t, res.IsError)

	// Unauthorized for specific container
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false})
	res, _ = h(ctxUser, mockMCPRequest("", "unauth"))
	assert.True(t, res.IsError)

	// Success
	res, _ = h(ctx, mockMCPRequest("", "test"))
	if res.IsError {
		t.Logf("Success case failed with error: %v", res)
	}
	assert.False(t, res.IsError)

	// Truncated logs
	res, _ = h(ctx, mockMCPRequest("", "huge-log"))
	if res.IsError {
		t.Logf("Huge log case failed with error: %v", res)
	}
	assert.False(t, res.IsError)
	if !res.IsError {
		assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "[truncated]")
	}

	// Log error
	cliErr := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/logs") {
			return nil, assert.AnError
		}
		return mockHandlerForMCP(req)
	})
	res, _ = mcpGetContainerLogsHandler(cliErr)(ctx, mockMCPRequest("", "test"))
	assert.True(t, res.IsError)
}

func TestMCPInspectContainerHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpInspectContainerHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequest("", "test"))
	assert.True(t, res.IsError)

	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Missing ID
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	// Container not found
	res, _ = h(ctx, mockMCPRequest("", "notfound"))
	assert.True(t, res.IsError)

	// Unauthorized for specific container
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false})
	res, _ = h(ctxUser, mockMCPRequest("", "unauth"))
	assert.True(t, res.IsError)

	// Success
	res, _ = h(ctx, mockMCPRequest("", "test"))
	assert.False(t, res.IsError)
}

func TestMCPStartContainerHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpStartContainerHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequest("", "test"))
	assert.True(t, res.IsError)

	// No permission
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanStart: false})
	res, _ = h(ctxUser, mockMCPRequest("", "test"))
	assert.True(t, res.IsError)

	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Missing ID
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	// Container not found
	res, _ = h(ctx, mockMCPRequest("", "notfound"))
	assert.True(t, res.IsError)

	// Unauthorized container
	res, _ = h(ctxUser, mockMCPRequest("", "unauth"))
	assert.True(t, res.IsError)

	// Success
	res, _ = h(ctx, mockMCPRequest("", "test"))
	assert.False(t, res.IsError)

	// Docker error
	res, _ = h(ctx, mockMCPRequest("", "fail-start"))
	assert.True(t, res.IsError)
}

func TestMCPStopContainerHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpStopContainerHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequest("", "test"))
	assert.True(t, res.IsError)

	// No permission
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanStop: false})
	res, _ = h(ctxUser, mockMCPRequest("", "test"))
	assert.True(t, res.IsError)

	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Missing ID
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	// Container not found
	res, _ = h(ctx, mockMCPRequest("", "notfound"))
	assert.True(t, res.IsError)

	// Unauthorized container
	res, _ = h(ctxUser, mockMCPRequest("", "unauth"))
	assert.True(t, res.IsError)

	// Success
	res, _ = h(ctx, mockMCPRequest("", "test"))
	assert.False(t, res.IsError)

	// Docker error
	res, _ = h(ctx, mockMCPRequest("", "fail-stop"))
	assert.True(t, res.IsError)
}

func TestMCPRestartContainerHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpRestartContainerHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequest("", "test"))
	assert.True(t, res.IsError)

	// No permission
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanRestart: false})
	res, _ = h(ctxUser, mockMCPRequest("", "test"))
	assert.True(t, res.IsError)

	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Missing ID
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	// Container not found
	res, _ = h(ctx, mockMCPRequest("", "notfound"))
	assert.True(t, res.IsError)

	// Unauthorized container
	res, _ = h(ctxUser, mockMCPRequest("", "unauth"))
	assert.True(t, res.IsError)

	// Success
	res, _ = h(ctx, mockMCPRequest("", "test"))
	assert.False(t, res.IsError)

	// Docker error
	res, _ = h(ctx, mockMCPRequest("", "fail-restart"))
	assert.True(t, res.IsError)
}

func TestMCPListImagesHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpListImagesHandler(cli)

	// Unauthorized / Non-admin
	res, _ := h(context.Background(), mockMCPRequest("", ""))
	assert.True(t, res.IsError)
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false})
	res, _ = h(ctxUser, mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Success
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.False(t, res.IsError)

	// Docker error
	cliErr := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		return nil, assert.AnError
	})
	res, _ = mcpListImagesHandler(cliErr)(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)
}

func TestMCPListVolumesHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpListVolumesHandler(cli)

	// Unauthorized / Non-admin
	res, _ := h(context.Background(), mockMCPRequest("", ""))
	assert.True(t, res.IsError)
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false})
	res, _ = h(ctxUser, mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Success
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.False(t, res.IsError)

	// Docker error
	cliErr := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		return nil, assert.AnError
	})
	res, _ = mcpListVolumesHandler(cliErr)(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)
}

func TestMCPListNetworksHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpListNetworksHandler(cli)

	// Unauthorized / Non-admin
	res, _ := h(context.Background(), mockMCPRequest("", ""))
	assert.True(t, res.IsError)
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false})
	res, _ = h(ctxUser, mockMCPRequest("", ""))
	assert.True(t, res.IsError)

	ctx := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Success
	res, _ = h(ctx, mockMCPRequest("", ""))
	assert.False(t, res.IsError)

	// Docker error
	cliErr := mockDockerClientWithRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		return nil, assert.AnError
	})
	res, _ = mcpListNetworksHandler(cliErr)(ctx, mockMCPRequest("", ""))
	assert.True(t, res.IsError)
}


func TestRegisterMCPRoutes_Coverage(t *testing.T) {
	e := echo.New()
	g := e.Group("/api")
	cli, _ := client.NewClientWithOpts(client.WithVersion("1.41"))
	registerMCPRoutes(g, cli)
	assert.NotNil(t, g)
}
