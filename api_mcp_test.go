package main

import (
	"context"
	"fmt"
	"lighthouse/db"
	"lighthouse/scanner"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/moby/moby/client"
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
	} else if req.Method == "GET" && strings.HasSuffix(path, "/stats") {
		if strings.Contains(path, "fail-stats") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusOK, `{"read":"2026-11-25T00:00:00Z","cpu_stats":{"cpu_usage":{"total_usage":1000},"system_cpu_usage":2000,"online_cpus":2},"precpu_stats":{"cpu_usage":{"total_usage":500},"system_cpu_usage":1000},"memory_stats":{"usage":102400,"limit":1048576},"networks":{"eth0":{"rx_bytes":100,"tx_bytes":200}},"blkio_stats":{"io_service_bytes_recursive":[{"op":"read","value":50},{"op":"write","value":60}]}}`), nil
	} else if req.Method == "DELETE" && strings.Contains(path, "/containers/") {
		if strings.Contains(path, "fail-del") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusNoContent, ``), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/exec") {
		if strings.Contains(path, "fail-exec") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusCreated, `{"Id": "exec123"}`), nil
	} else if req.Method == "POST" && strings.Contains(path, "/exec/") && strings.HasSuffix(path, "/start") {
		hdr := []byte{1, 0, 0, 0, 0, 0, 0, 12}
		return makeResponse(http.StatusOK, string(hdr)+"command out\n"), nil
	} else if req.Method == "GET" && strings.Contains(path, "/exec/") && strings.HasSuffix(path, "/json") {
		return makeResponse(http.StatusOK, `{"ExitCode": 0, "Running": false}`), nil
	} else if req.Method == "GET" && strings.Contains(path, "/images/") && strings.HasSuffix(path, "/json") {
		if strings.Contains(path, "notfound") {
			return makeResponse(http.StatusNotFound, `{"message": "no such image"}`), nil
		}
		return makeResponse(http.StatusOK, `{"Id": "img1", "RepoTags": ["alpine:latest"], "Size": 5000000, "Os": "linux", "Architecture": "arm64"}`), nil
	} else if req.Method == "DELETE" && strings.Contains(path, "/images/") {
		if strings.Contains(path, "fail-del-img") {
			return nil, assert.AnError
		}
		return makeResponse(http.StatusOK, `[{"Untagged": "alpine:latest"}]`), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/images/prune") {
		return makeResponse(http.StatusOK, `{"ImagesDeleted": [{"Untagged": "old:1"}], "SpaceReclaimed": 1024}`), nil
	} else if req.Method == "GET" && strings.Contains(path, "/volumes/") {
		if strings.Contains(path, "notfound") {
			return makeResponse(http.StatusNotFound, `{"message": "no such volume"}`), nil
		}
		return makeResponse(http.StatusOK, `{"Name": "test-vol", "Driver": "local", "Mountpoint": "/var/lib/docker/volumes/test-vol/_data"}`), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/volumes/prune") {
		return makeResponse(http.StatusOK, `{"VolumesDeleted": ["old-vol"], "SpaceReclaimed": 2048}`), nil
	} else if req.Method == "GET" && strings.Contains(path, "/networks/") {
		if strings.Contains(path, "notfound") {
			return makeResponse(http.StatusNotFound, `{"message": "no such network"}`), nil
		}
		return makeResponse(http.StatusOK, `{"Id": "net1", "Name": "bridge", "Driver": "bridge", "Scope": "local"}`), nil
	} else if req.Method == "POST" && strings.HasSuffix(path, "/networks/prune") {
		return makeResponse(http.StatusOK, `{"NetworksDeleted": ["old-net"]}`), nil
	} else if req.Method == "GET" && strings.Contains(path, "/containers/") && strings.HasSuffix(path, "/json") {
		// inspect container
		if strings.Contains(path, "notfound") {
			return makeResponse(http.StatusNotFound, `{"message": "no such container"}`), nil
		}
		if strings.Contains(path, "unauth") {
			return makeResponse(http.StatusOK, `{"Id": "unauth", "Name": "/unauth", "Config": {"Image": "unauth-img"}}`), nil
		}
		if strings.Contains(path, "lh-self") {
			return makeResponse(http.StatusOK, `{"Id": "lh-self", "Name": "/lighthouse", "Config": {"Image": "lighthouse"}}`), nil
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

	e.ServeHTTP(rec, req)
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

// TestRegisterMCPRoutes_InjectClaimsWithValidToken covers the injectClaims
// middleware branch where a real *jwt.Token with *UserClaims is present on
// the echo context (via the group's auth middleware), so it gets threaded
// onto the outgoing http.Request context (the other test only exercises
// the no-token path).
func TestRegisterMCPRoutes_InjectClaimsWithValidToken(t *testing.T) {
	claims := &UserClaims{ID: 1, IsAdmin: true}
	e, g, _, _ := setupEchoWithClaimsHelper(claims)
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	registerMCPRoutes(g, cli)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/sse", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func mockMCPRequestWithMap(method string, args map[string]interface{}) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Method = method
	if args == nil {
		req.Params.Arguments = map[string]interface{}{}
	} else {
		req.Params.Arguments = args
	}
	return req
}

func TestMCPTriggerVulnerabilityScanHandler(t *testing.T) {
	setupMCPTestDB()
	db.GormDB.Create(&db.User{ID: 3, Username: "scanuser", Email: "scanuser@example.com", IsAdmin: false, CanRunScans: true, AllowedContainers: "c1"})

	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpTriggerVulnerabilityScanHandler(cli)

	// Unauthorized (no claims)
	res, _ := h(context.Background(), mockMCPRequestWithMap("trigger_vulnerability_scan", nil))
	assert.True(t, res.IsError)

	// User without scan permissions (User 2)
	ctxUserNoScan := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanRunScans: false})
	res, _ = h(ctxUserNoScan, mockMCPRequestWithMap("trigger_vulnerability_scan", map[string]interface{}{"image_name": "nginx:latest"}))
	assert.True(t, res.IsError)

	// User with scan permissions (User 3)
	ctxScanUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 3, IsAdmin: false, CanRunScans: true})

	// Missing both container_id and image_name
	res, _ = h(ctxScanUser, mockMCPRequestWithMap("trigger_vulnerability_scan", map[string]interface{}{}))
	assert.True(t, res.IsError)

	// Invalid image reference
	res, _ = h(ctxScanUser, mockMCPRequestWithMap("trigger_vulnerability_scan", map[string]interface{}{"image_name": "--invalid-flag"}))
	assert.True(t, res.IsError)

	// Mock scanner function
	origScan := scanner.ScanImageFunc
	scanner.ScanImageFunc = func(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
		return map[string]interface{}{
			"Results": []interface{}{
				map[string]interface{}{
					"Target": "alpine:latest",
					"Vulnerabilities": []interface{}{
						map[string]interface{}{
							"VulnerabilityID": "CVE-2024-9999",
							"Severity":        "CRITICAL",
							"PkgName":         "openssl",
						},
					},
				},
			},
		}, nil
	}
	defer func() { scanner.ScanImageFunc = origScan }()

	// Mock background scanner function to prevent background goroutines from racing with subsequent tests
	bgScanCalls := 0
	origBgScan := mcpRunBackgroundScan
	mcpRunBackgroundScan = func(cli *client.Client, imageName string) {
		bgScanCalls++
	}
	defer func() { mcpRunBackgroundScan = origBgScan }()

	// Admin triggering scan with wait=true
	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("trigger_vulnerability_scan", map[string]interface{}{
		"image_name": "alpine:latest",
		"wait":       true,
	}))
	assert.False(t, res.IsError)

	// Admin triggering scan in background
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("trigger_vulnerability_scan", map[string]interface{}{
		"image_name": "alpine:latest",
		"wait":       false,
	}))
	assert.False(t, res.IsError)

	// Triggering via container_id
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("trigger_vulnerability_scan", map[string]interface{}{
		"container_id": "c1",
		"wait":         false,
	}))
	assert.False(t, res.IsError)
	assert.Equal(t, 2, bgScanCalls)
}

func TestMCPListVulnerabilityScansHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpListVulnerabilityScansHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequestWithMap("list_vulnerability_scans", nil))
	assert.True(t, res.IsError)

	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Empty list
	db.GormDB.Exec("DELETE FROM image_scan_results")
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("list_vulnerability_scans", nil))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "No vulnerability scans found")

	// Insert scan results
	db.GormDB.Create(&db.ImageScanResult{
		Image:     "alpine:latest",
		Result:    `{"Results":[{"Target":"alpine:latest","Vulnerabilities":[{"VulnerabilityID":"CVE-1","Severity":"HIGH"}]}]}`,
		CreatedAt: time.Now(),
	})
	db.GormDB.Create(&db.ImageScanResult{
		Image:     "nginx:latest",
		Result:    `{"Error":"failed to download image"}`,
		CreatedAt: time.Now(),
	})

	// List all
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("list_vulnerability_scans", map[string]interface{}{"limit": 10}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "alpine:latest")
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "nginx:latest")

	// Filter by image
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("list_vulnerability_scans", map[string]interface{}{"image_name": "alpine:latest"}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "alpine:latest")
	assert.NotContains(t, res.Content[0].(mcp.TextContent).Text, "nginx:latest")
}

func TestMCPGetVulnerabilityScanHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpGetVulnerabilityScanHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequestWithMap("get_vulnerability_scan", nil))
	assert.True(t, res.IsError)

	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Missing parameters
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("get_vulnerability_scan", map[string]interface{}{}))
	assert.True(t, res.IsError)

	// Scan not found
	db.GormDB.Exec("DELETE FROM image_scan_results")
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("get_vulnerability_scan", map[string]interface{}{"image_name": "nonexistent:latest"}))
	assert.True(t, res.IsError)

	// Insert scan result with CVEs
	db.GormDB.Create(&db.ImageScanResult{
		Image: "redis:alpine",
		Result: `{
			"Results": [
				{
					"Target": "redis:alpine",
					"Vulnerabilities": [
						{
							"VulnerabilityID": "CVE-2024-1111",
							"PkgName": "libssl3",
							"InstalledVersion": "3.0.0",
							"FixedVersion": "3.0.1",
							"Severity": "CRITICAL",
							"Title": "Memory corruption in OpenSSL"
						},
						{
							"VulnerabilityID": "CVE-2024-2222",
							"PkgName": "curl",
							"InstalledVersion": "7.8.0",
							"Severity": "MEDIUM",
							"Title": "Buffer overflow"
						}
					]
				}
			]
		}`,
		CreatedAt: time.Now(),
	})

	// Get full report
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("get_vulnerability_scan", map[string]interface{}{"image_name": "redis:alpine"}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "CVE-2024-1111")
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "CVE-2024-2222")

	// Get with severity filter (CRITICAL only)
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("get_vulnerability_scan", map[string]interface{}{
		"image_name": "redis:alpine",
		"severity":   "CRITICAL",
	}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "CVE-2024-1111")
	assert.NotContains(t, res.Content[0].(mcp.TextContent).Text, "CVE-2024-2222")

	// Insert error scan result
	db.GormDB.Create(&db.ImageScanResult{
		Image:     "broken:latest",
		Result:    `{"Error": "Scan execution timed out"}`,
		CreatedAt: time.Now(),
	})
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("get_vulnerability_scan", map[string]interface{}{"image_name": "broken:latest"}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "Scan execution timed out")
}

func TestMCPDeleteContainerHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpDeleteContainerHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequestWithMap("delete_container", map[string]interface{}{"container_id": "c1"}))
	assert.True(t, res.IsError)

	// User lacks delete permission
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanDelete: false})
	res, _ = h(ctxUser, mockMCPRequestWithMap("delete_container", map[string]interface{}{"container_id": "c1"}))
	assert.True(t, res.IsError)

	// Admin context
	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true, CanDelete: true})

	// Missing container_id
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("delete_container", map[string]interface{}{}))
	assert.True(t, res.IsError)

	// Invalid ID format
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("delete_container", map[string]interface{}{"container_id": "bad;id"}))
	assert.True(t, res.IsError)

	// Container not found
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("delete_container", map[string]interface{}{"container_id": "notfound"}))
	assert.True(t, res.IsError)

	// Attempt to delete platform self container
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("delete_container", map[string]interface{}{"container_id": "lh-self"}))
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "Cannot delete the LightHouse platform container")

	// Success
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("delete_container", map[string]interface{}{"container_id": "c1", "force": true}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "deleted successfully")
}

func TestMCPGetContainerStatsHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpGetContainerStatsHandler(cli)

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequestWithMap("get_container_stats", map[string]interface{}{"container_id": "c1"}))
	assert.True(t, res.IsError)

	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true})

	// Missing container_id
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("get_container_stats", map[string]interface{}{}))
	assert.True(t, res.IsError)

	// Container not found
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("get_container_stats", map[string]interface{}{"container_id": "notfound"}))
	assert.True(t, res.IsError)

	// Success
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("get_container_stats", map[string]interface{}{"container_id": "c1"}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "cpu_percent")
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "memory_usage_bytes")
}

func TestMCPExecuteContainerCommandHandler(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)
	h := mcpExecuteContainerCommandHandler(cli)

	origAllowShell := AllowShell
	AllowShell = true
	defer func() { AllowShell = origAllowShell }()

	origExecRunner := mcpExecCommandRunner
	mcpExecCommandRunner = func(ctx context.Context, cli *client.Client, containerID string, command string) (int, string, string, error) {
		if command == "fail" {
			return 1, "", "error output", fmt.Errorf("command execution failed: exit status 1")
		}
		return 0, "hello world\n", "", nil
	}
	defer func() { mcpExecCommandRunner = origExecRunner }()

	// Unauthorized
	res, _ := h(context.Background(), mockMCPRequestWithMap("execute_container_command", map[string]interface{}{"container_id": "c1", "command": "echo hi"}))
	assert.True(t, res.IsError)

	// AllowShell false
	AllowShell = false
	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true, CanShell: true})
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("execute_container_command", map[string]interface{}{"container_id": "c1", "command": "echo hi"}))
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "Shell execution is disabled")
	AllowShell = true

	// User lacks can_shell
	ctxUserNoShell := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanShell: false})
	res, _ = h(ctxUserNoShell, mockMCPRequestWithMap("execute_container_command", map[string]interface{}{"container_id": "c1", "command": "echo hi"}))
	assert.True(t, res.IsError)

	// Missing command
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("execute_container_command", map[string]interface{}{"container_id": "c1"}))
	assert.True(t, res.IsError)

	// Attempt on platform self container
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("execute_container_command", map[string]interface{}{"container_id": "lh-self", "command": "echo hi"}))
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "Cannot execute commands inside the LightHouse platform container")

	// Success
	res, _ = h(ctxAdmin, mockMCPRequestWithMap("execute_container_command", map[string]interface{}{"container_id": "c1", "command": "echo hello", "timeout": 10}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "exit_code")
}

func TestMCPImageHandlers(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)

	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true, CanDelete: true})
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanDelete: false})

	// Inspect Image
	inspectH := mcpInspectImageHandler(cli)
	res, _ := inspectH(ctxUser, mockMCPRequestWithMap("inspect_image", map[string]interface{}{"image_name": "alpine:latest"}))
	assert.True(t, res.IsError) // Admin only

	res, _ = inspectH(ctxAdmin, mockMCPRequestWithMap("inspect_image", map[string]interface{}{"image_name": "alpine:latest"}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "alpine:latest")

	// Remove Image
	removeH := mcpRemoveImageHandler(cli)
	res, _ = removeH(ctxUser, mockMCPRequestWithMap("remove_image", map[string]interface{}{"image_name": "alpine:latest"}))
	assert.True(t, res.IsError) // User lacks delete

	res, _ = removeH(ctxAdmin, mockMCPRequestWithMap("remove_image", map[string]interface{}{"image_name": "alpine:latest"}))
	assert.False(t, res.IsError)

	// Prune Images
	pruneH := mcpPruneImagesHandler(cli)
	res, _ = pruneH(ctxUser, mockMCPRequestWithMap("prune_images", map[string]interface{}{}))
	assert.True(t, res.IsError)

	res, _ = pruneH(ctxAdmin, mockMCPRequestWithMap("prune_images", map[string]interface{}{"all_unused": true}))
	assert.False(t, res.IsError)
}

func TestMCPVolumeAndNetworkHandlers(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)

	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true, CanDelete: true})
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanDelete: false})

	// Volume Inspect
	volInspectH := mcpInspectVolumeHandler(cli)
	res, _ := volInspectH(ctxUser, mockMCPRequestWithMap("inspect_volume", map[string]interface{}{"volume_name": "test-vol"}))
	assert.True(t, res.IsError)

	res, _ = volInspectH(ctxAdmin, mockMCPRequestWithMap("inspect_volume", map[string]interface{}{"volume_name": "test-vol"}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "test-vol")

	// Volume Prune
	volPruneH := mcpPruneVolumesHandler(cli)
	res, _ = volPruneH(ctxUser, mockMCPRequestWithMap("prune_volumes", map[string]interface{}{}))
	assert.True(t, res.IsError)

	res, _ = volPruneH(ctxAdmin, mockMCPRequestWithMap("prune_volumes", map[string]interface{}{}))
	assert.False(t, res.IsError)

	// Network Inspect
	netInspectH := mcpInspectNetworkHandler(cli)
	res, _ = netInspectH(ctxUser, mockMCPRequestWithMap("inspect_network", map[string]interface{}{"network_id": "net1"}))
	assert.True(t, res.IsError)

	res, _ = netInspectH(ctxAdmin, mockMCPRequestWithMap("inspect_network", map[string]interface{}{"network_id": "net1"}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "bridge")

	// Network Prune
	netPruneH := mcpPruneNetworksHandler(cli)
	res, _ = netPruneH(ctxUser, mockMCPRequestWithMap("prune_networks", map[string]interface{}{}))
	assert.True(t, res.IsError)

	res, _ = netPruneH(ctxAdmin, mockMCPRequestWithMap("prune_networks", map[string]interface{}{}))
	assert.False(t, res.IsError)
}

func TestMCPGitopsHandlers(t *testing.T) {
	setupMCPTestDB()

	project := db.GitProject{
		ID:         10,
		Name:       "web-app",
		SourceType: "git",
		RepoURL:    "https://github.com/example/web.git",
		Branch:     "main",
		AuthToken:  "secret-pat",
		Status:     "synced",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	db.GormDB.Create(&project)

	deployment := db.GitDeployment{
		ID:        1,
		ProjectID: 10,
		CommitSHA: "abc1234",
		Status:    "success",
		Logs:      "Container web-app started successfully",
		CreatedAt: time.Now(),
	}
	db.GormDB.Create(&deployment)

	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true, CanEditDeployments: true})
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, AllowedContainers: "c1"})

	// List GitOps
	listH := mcpListGitopsPipelinesHandler()
	res, _ := listH(ctxAdmin, mockMCPRequestWithMap("list_gitops_pipelines", nil))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "web-app")

	// Non-admin with pattern mismatch should see empty
	res, _ = listH(ctxUser, mockMCPRequestWithMap("list_gitops_pipelines", nil))
	assert.False(t, res.IsError)
	assert.NotContains(t, res.Content[0].(mcp.TextContent).Text, "web-app")

	// Get GitOps Pipeline
	getH := mcpGetGitopsPipelineHandler()
	res, _ = getH(ctxAdmin, mockMCPRequestWithMap("get_gitops_pipeline", map[string]interface{}{"pipeline_id": 10}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "web-app")
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "***") // token masked

	// Trigger GitOps Deploy
	deployH := mcpTriggerGitopsDeployHandler()
	res, _ = deployH(ctxUser, mockMCPRequestWithMap("trigger_gitops_deploy", map[string]interface{}{"pipeline_id": 10}))
	assert.True(t, res.IsError) // user lacks permission

	res, _ = deployH(ctxAdmin, mockMCPRequestWithMap("trigger_gitops_deploy", map[string]interface{}{"pipeline_id": 10}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "GitOps deployment triggered")

	// Get GitOps History
	historyH := mcpGetGitopsHistoryHandler()
	res, _ = historyH(ctxAdmin, mockMCPRequestWithMap("get_gitops_history", map[string]interface{}{"pipeline_id": 10, "limit": 5}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "abc1234")
}

func TestMCPClusterAndAlertHandlers(t *testing.T) {
	setupMCPTestDB()

	rule := db.AlertRule{
		Name:             "Custom Crash Detector",
		ContainerPattern: ".*",
		EventTypes:       "die",
		Enabled:          true,
		CooldownSeconds:  120,
	}
	db.GormDB.Create(&rule)

	incident := db.AlertHistory{
		RuleID:         &rule.ID,
		RuleName:       "Custom Crash Detector",
		ContainerName:  "c1",
		AlertType:      "die",
		DeliveryStatus: "sent",
		Timestamp:      time.Now(),
	}
	db.GormDB.Create(&incident)

	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true, CanViewSystemHealth: true})
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanViewSystemHealth: false})

	// List Cluster Nodes
	nodesH := mcpListClusterNodesHandler()
	res, _ := nodesH(ctxUser, mockMCPRequestWithMap("list_cluster_nodes", nil))
	assert.True(t, res.IsError)

	res, _ = nodesH(ctxAdmin, mockMCPRequestWithMap("list_cluster_nodes", nil))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "capabilities")

	// Get Node Details
	nodeDetailH := mcpGetNodeDetailsHandler()
	res, _ = nodeDetailH(ctxAdmin, mockMCPRequestWithMap("get_node_details", map[string]interface{}{"node_id": "local"}))
	assert.False(t, res.IsError)

	// List Alert Rules
	alertRulesH := mcpListAlertRulesHandler()
	res, _ = alertRulesH(ctxUser, mockMCPRequestWithMap("list_alert_rules", nil))
	assert.True(t, res.IsError) // Admin only

	res, _ = alertRulesH(ctxAdmin, mockMCPRequestWithMap("list_alert_rules", nil))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "Custom Crash Detector")

	// Get Alert History
	alertHistH := mcpGetAlertHistoryHandler()
	res, _ = alertHistH(ctxUser, mockMCPRequestWithMap("get_alert_history", nil))
	assert.True(t, res.IsError)

	res, _ = alertHistH(ctxAdmin, mockMCPRequestWithMap("get_alert_history", map[string]interface{}{"limit": 10}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "Custom Crash Detector")
}

func TestMCPAuditAndSystemInfoHandlers(t *testing.T) {
	setupMCPTestDB()
	cli := mockDockerClientWithRoundTripper(t, mockHandlerForMCP)

	audit := db.AuditLog{
		ID:        1,
		UserID:    1,
		Username:  "admin",
		Action:    "START",
		Resource:  "Container:c1",
		Status:    "Success",
		Message:   "Started container c1",
		Timestamp: time.Now(),
	}
	db.GormDB.Create(&audit)

	ctxAdmin := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 1, IsAdmin: true, CanViewSystemHealth: true})
	ctxUser := context.WithValue(context.Background(), "userClaims", &UserClaims{ID: 2, IsAdmin: false, CanViewSystemHealth: false})

	// Audit Logs
	auditH := mcpGetAuditLogsHandler()
	res, _ := auditH(ctxUser, mockMCPRequestWithMap("get_audit_logs", nil))
	assert.True(t, res.IsError) // Admin only

	res, _ = auditH(ctxAdmin, mockMCPRequestWithMap("get_audit_logs", map[string]interface{}{"limit": 20, "action": "START"}))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "Container:c1")

	// System Info
	sysH := mcpGetSystemInfoHandler(cli)
	res, _ = sysH(ctxUser, mockMCPRequestWithMap("get_system_info", nil))
	assert.True(t, res.IsError)

	res, _ = sysH(ctxAdmin, mockMCPRequestWithMap("get_system_info", nil))
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "cpu_cores")
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "allow_start")
}
