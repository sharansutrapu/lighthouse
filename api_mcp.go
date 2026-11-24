package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
	"time"

	"gorm.io/gorm"

	"lighthouse/cluster"
	"lighthouse/db"
	"lighthouse/scanner"
)

// registerMCPRoutes exposes LightHouse's Docker operations as MCP (Model
// Context Protocol) tools over an SSE transport, so AI agents can list,
// inspect, and control containers. Every tool call is bound to the same
// RBAC/visibility rules as the human user who issued the API token used to
// authenticate (see docs/SECURITY.md, "MCP Server Security").
func registerMCPRoutes(r *echo.Group, cli *client.Client) {
	// Create MCP server
	mcpServer := server.NewMCPServer(
		"Lighthouse",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Add tools
	mcpServer.AddTool(mcp.NewTool("list_containers",
		mcp.WithDescription("List Docker containers that the authenticated user has access to."),
	), mcpListContainersHandler(cli))

	mcpServer.AddTool(mcp.NewTool("get_container_logs",
		mcp.WithDescription("Fetch the stdout and stderr logs for a specific container."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container.")),
	), mcpGetContainerLogsHandler(cli))

	mcpServer.AddTool(mcp.NewTool("inspect_container",
		mcp.WithDescription("Inspect detailed information about a specific container."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container.")),
	), mcpInspectContainerHandler(cli))

	// Lifecycle Tools
	mcpServer.AddTool(mcp.NewTool("start_container",
		mcp.WithDescription("Start a specific container."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container to start.")),
	), mcpStartContainerHandler(cli))

	mcpServer.AddTool(mcp.NewTool("stop_container",
		mcp.WithDescription("Stop a specific container."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container to stop.")),
	), mcpStopContainerHandler(cli))

	mcpServer.AddTool(mcp.NewTool("restart_container",
		mcp.WithDescription("Restart a specific container."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container to restart.")),
	), mcpRestartContainerHandler(cli))

	// System Resource Tools (Admin Only)
	mcpServer.AddTool(mcp.NewTool("list_images",
		mcp.WithDescription("List available Docker images (Admin only)."),
	), mcpListImagesHandler(cli))

	mcpServer.AddTool(mcp.NewTool("list_volumes",
		mcp.WithDescription("List Docker volumes (Admin only)."),
	), mcpListVolumesHandler(cli))

	mcpServer.AddTool(mcp.NewTool("list_networks",
		mcp.WithDescription("List Docker networks (Admin only)."),
	), mcpListNetworksHandler(cli))

	// Vulnerability Scanning Tools
	mcpServer.AddTool(mcp.NewTool("trigger_vulnerability_scan",
		mcp.WithDescription("Trigger a Trivy vulnerability scan for a container's image or directly on an image reference."),
		mcp.WithString("container_id", mcp.Description("Optional container ID or name to scan.")),
		mcp.WithString("image_name", mcp.Description("Optional Docker image name to scan (e.g. 'nginx:latest').")),
		mcp.WithBoolean("wait", mcp.Description("Whether to wait for the scan to finish before returning. Defaults to false (runs in background).")),
	), mcpTriggerVulnerabilityScanHandler(cli))

	mcpServer.AddTool(mcp.NewTool("list_vulnerability_scans",
		mcp.WithDescription("List saved vulnerability scan records and severity counts."),
		mcp.WithString("image_name", mcp.Description("Optional image name to filter scans.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of records to return (default 20, max 100).")),
	), mcpListVulnerabilityScansHandler(cli))

	mcpServer.AddTool(mcp.NewTool("get_vulnerability_scan",
		mcp.WithDescription("Get detailed vulnerability scan results and CVE reports for an image or container."),
		mcp.WithString("image_name", mcp.Description("Docker image name to get scan results for.")),
		mcp.WithString("container_id", mcp.Description("Optional container ID or name to get scan results for.")),
		mcp.WithString("severity", mcp.Description("Optional severity filter (e.g. 'CRITICAL', 'HIGH', 'MEDIUM', 'LOW').")),
	), mcpGetVulnerabilityScanHandler(cli))

	// Create SSE server
	sseServer := server.NewSSEServer(mcpServer,
		server.WithStaticBasePath("/api/mcp"),
		server.WithSSEContextFunc(func(ctx context.Context, req *http.Request) context.Context {
			// Extract claims from the request context if injected
			if claims, ok := req.Context().Value("userClaims").(*UserClaims); ok {
				return context.WithValue(ctx, "userClaims", claims)
			}
			return ctx
		}),
	)

	// Middleware to extract JWT claims and put into http.Request context
	injectClaims := func(next http.Handler) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Ensure Nginx and other proxies don't buffer the SSE stream
			c.Response().Header().Set("X-Accel-Buffering", "no")

			if token, ok := c.Get("user").(*jwt.Token); ok {
				if claims, ok := token.Claims.(*UserClaims); ok {
					ctx := context.WithValue(c.Request().Context(), "userClaims", claims)
					c.SetRequest(c.Request().WithContext(ctx))
				}
			}
			next.ServeHTTP(c.Response(), c.Request())
			return nil
		}
	}

	// Register MCP SSE endpoints
	r.GET("/mcp/sse", injectClaims(sseServer.SSEHandler()))
	r.POST("/mcp/message", injectClaims(sseServer.MessageHandler()))
}

// Helper to check if a user is authorized for a specific container name
func isMCPContainerAuthorized(userID int, isAdmin bool, containerName string, imageName string) bool {
	containerName = strings.TrimPrefix(containerName, "/")

	if isLightHouseSelfContainer(containerName, imageName) {
		return false // Never expose lighthouse platform container
	}
	if inspectContainerExcluded(isAdmin, containerName, imageName) {
		return false
	}

	if isAdmin {
		return true
	}

	patterns := getAuthorizedPatterns(userID)
	for _, p := range patterns {
		if p.MatchString(containerName) {
			return true
		}
	}
	return false
}

// Helper to fetch the latest DB admin status (in case JWT is stale)
func getMCPUserIsAdmin(userID int) bool {
	var u db.User
	if err := db.GormDB.Select("is_admin").First(&u, userID).Error; err != nil {
		return false
	}
	return u.IsAdmin
}

// mcpListContainersHandler implements the "list_containers" MCP tool,
// returning only containers visible under the caller's permissions.
func mcpListContainersHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		var patterns []*regexp.Regexp
		if !isAdmin {
			patterns = getAuthorizedPatterns(claims.ID)
		}

		res, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list containers: %v", err)), nil
		}

		var visibleContainers []map[string]interface{}
		for _, ctr := range res.Items {
			name := "unknown"
			if len(ctr.Names) > 0 {
				name = strings.TrimPrefix(ctr.Names[0], "/")
			}

			if isLightHouseSelfContainer(name, ctr.Image) || isExcludedContainer(name, ctr.Image) {
				continue
			}

			visible := isAdmin
			if !visible {
				for _, p := range patterns {
					if p.MatchString(name) {
						visible = true
						break
					}
				}
			}

			if visible {
				visibleContainers = append(visibleContainers, map[string]interface{}{
					"ID":     ctr.ID,
					"Name":   name,
					"Image":  ctr.Image,
					"State":  ctr.State,
					"Status": ctr.Status,
				})
			}
		}

		b, _ := json.MarshalIndent(visibleContainers, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpGetContainerLogsHandler implements the "get_container_logs" MCP tool,
// returning the last 100 lines of stdout/stderr, truncated to 50KB total to
// avoid overwhelming the calling AI agent's context window.
func mcpGetContainerLogsHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		containerID, err := request.RequireString("container_id")
		if err != nil {
			return mcp.NewToolResultError("container_id is required"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)

		container, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Container not found: %v", err)), nil
		}

		image := ""
		if container.Container.Config != nil {
			image = container.Container.Config.Image
		}

		if !isMCPContainerAuthorized(claims.ID, isAdmin, container.Container.Name, image) {
			return mcp.NewToolResultError("Unauthorized access to this container"), nil
		}

		out, err := cli.ContainerLogs(ctx, container.Container.ID, client.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       "100", // Limit to last 100 lines to prevent OOM
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get logs: %v", err)), nil
		}
		defer out.Close()

		stdout := new(strings.Builder)
		stderr := new(strings.Builder)
		_, err = stdcopy.StdCopy(stdout, stderr, out)
		if err != nil {
			log.Printf("Error copying logs: %v", err)
		}

		// Cap output at 50KB to prevent large responses from overwhelming MCP clients
		const maxBytes = 50 * 1024
		stdoutStr := stdout.String()
		stderrStr := stderr.String()
		if len(stdoutStr)+len(stderrStr) > maxBytes {
			stdoutStr = stdoutStr[:min(len(stdoutStr), maxBytes/2)] + "\n... [truncated]"
			stderrStr = stderrStr[:min(len(stderrStr), maxBytes/2)] + "\n... [truncated]"
		}

		result := fmt.Sprintf("=== STDOUT ===\n%s\n=== STDERR ===\n%s", stdoutStr, stderrStr)
		return mcp.NewToolResultText(result), nil
	}
}

// mcpInspectContainerHandler implements the "inspect_container" MCP tool.
func mcpInspectContainerHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		containerID, err := request.RequireString("container_id")
		if err != nil {
			return mcp.NewToolResultError("container_id is required"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)

		container, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("Container not found"), nil
		}

		image := ""
		if container.Container.Config != nil {
			image = container.Container.Config.Image
		}

		if !isMCPContainerAuthorized(claims.ID, isAdmin, container.Container.Name, image) {
			return mcp.NewToolResultError("Unauthorized access to this container"), nil
		}

		sanitizeContainerInspect(&container)
		b, _ := json.MarshalIndent(container.Container, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// -- Lifecycle Management --

// mcpStartContainerHandler implements the "start_container" MCP tool.
func mcpStartContainerHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanStart {
			return mcp.NewToolResultError("Unauthorized: User lacks Start permission"), nil
		}

		containerID, err := request.RequireString("container_id")
		if err != nil {
			return mcp.NewToolResultError("container_id is required"), nil
		}

		container, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("Container not found"), nil
		}

		image := ""
		if container.Container.Config != nil {
			image = container.Container.Config.Image
		}
		if !isMCPContainerAuthorized(claims.ID, isAdmin, container.Container.Name, image) {
			return mcp.NewToolResultError("Unauthorized to start this container"), nil
		}

		if _, err := cli.ContainerStart(ctx, container.Container.ID, client.ContainerStartOptions{}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to start container: %v", err)), nil
		}
		return mcp.NewToolResultText("Container started successfully"), nil
	}
}

// mcpStopContainerHandler implements the "stop_container" MCP tool.
func mcpStopContainerHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanStop {
			return mcp.NewToolResultError("Unauthorized: User lacks Stop permission"), nil
		}

		containerID, err := request.RequireString("container_id")
		if err != nil {
			return mcp.NewToolResultError("container_id is required"), nil
		}

		container, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("Container not found"), nil
		}

		image := ""
		if container.Container.Config != nil {
			image = container.Container.Config.Image
		}
		if !isMCPContainerAuthorized(claims.ID, isAdmin, container.Container.Name, image) {
			return mcp.NewToolResultError("Unauthorized to stop this container"), nil
		}

		if _, err := cli.ContainerStop(ctx, container.Container.ID, client.ContainerStopOptions{}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to stop container: %v", err)), nil
		}
		return mcp.NewToolResultText("Container stopped successfully"), nil
	}
}

// mcpRestartContainerHandler implements the "restart_container" MCP tool.
func mcpRestartContainerHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanRestart {
			return mcp.NewToolResultError("Unauthorized: User lacks Restart permission"), nil
		}

		containerID, err := request.RequireString("container_id")
		if err != nil {
			return mcp.NewToolResultError("container_id is required"), nil
		}

		container, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("Container not found"), nil
		}

		image := ""
		if container.Container.Config != nil {
			image = container.Container.Config.Image
		}
		if !isMCPContainerAuthorized(claims.ID, isAdmin, container.Container.Name, image) {
			return mcp.NewToolResultError("Unauthorized to restart this container"), nil
		}

		if _, err := cli.ContainerRestart(ctx, container.Container.ID, client.ContainerRestartOptions{}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to restart container: %v", err)), nil
		}
		return mcp.NewToolResultText("Container restarted successfully"), nil
	}
}

// -- System Resources (Admin Only) --

// mcpListImagesHandler implements the admin-only "list_images" MCP tool.
func mcpListImagesHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok || !getMCPUserIsAdmin(claims.ID) {
			return mcp.NewToolResultError("Unauthorized: Admin only"), nil
		}

		res, err := cli.ImageList(ctx, client.ImageListOptions{All: false})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list images: %v", err)), nil
		}

		b, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpListVolumesHandler implements the admin-only "list_volumes" MCP tool.
func mcpListVolumesHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok || !getMCPUserIsAdmin(claims.ID) {
			return mcp.NewToolResultError("Unauthorized: Admin only"), nil
		}

		res, err := cli.VolumeList(ctx, client.VolumeListOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list volumes: %v", err)), nil
		}

		b, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpListNetworksHandler implements the admin-only "list_networks" MCP tool.
func mcpListNetworksHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok || !getMCPUserIsAdmin(claims.ID) {
			return mcp.NewToolResultError("Unauthorized: Admin only"), nil
		}

		res, err := cli.NetworkList(ctx, client.NetworkListOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list networks: %v", err)), nil
		}

		b, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// -- Vulnerability Scanning Helpers & Handlers --

type mcpVulnSummary struct {
	TotalCount int `json:"total_count"`
	Critical   int `json:"critical"`
	High       int `json:"high"`
	Medium     int `json:"medium"`
	Low        int `json:"low"`
	Unknown    int `json:"unknown"`
}

func getMCPStringParam(req mcp.CallToolRequest, key string) string {
	if req.Params.Arguments == nil {
		return ""
	}
	if v, ok := req.Params.Arguments[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func getMCPBoolParam(req mcp.CallToolRequest, key string) bool {
	if req.Params.Arguments == nil {
		return false
	}
	if v, ok := req.Params.Arguments[key]; ok && v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
		if s, ok := v.(string); ok {
			return strings.ToLower(strings.TrimSpace(s)) == "true"
		}
	}
	return false
}

func getMCPIntParam(req mcp.CallToolRequest, key string, defaultValue int) int {
	if req.Params.Arguments == nil {
		return defaultValue
	}
	if v, ok := req.Params.Arguments[key]; ok && v != nil {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		case int64:
			return int(val)
		case string:
			var i int
			if _, err := fmt.Sscanf(strings.TrimSpace(val), "%d", &i); err == nil && i > 0 {
				return i
			}
		}
	}
	return defaultValue
}

func isUserScanAuthorized(claims *UserClaims, isAdmin bool) bool {
	if isAdmin || claims.IsAdmin || claims.CanRunScans {
		return true
	}
	var u db.User
	if err := db.GormDB.Select("is_admin, can_run_scans, team_id").Preload("Team").First(&u, claims.ID).Error; err == nil {
		if u.IsAdmin || u.CanRunScans || (u.Team != nil && u.Team.CanRunScans) {
			return true
		}
	}
	return false
}

func getMCPUserAuthorizedImages(ctx context.Context, cli *client.Client, userID int, isAdmin bool) (map[string]bool, error) {
	if isAdmin {
		return nil, nil // nil means all images permitted
	}
	authorized := make(map[string]bool)
	patterns := getAuthorizedPatterns(userID)
	if cli != nil {
		res, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err == nil {
			for _, ctr := range res.Items {
				name := "unknown"
				if len(ctr.Names) > 0 {
					name = strings.TrimPrefix(ctr.Names[0], "/")
				}
				if isLightHouseSelfContainer(name, ctr.Image) || isExcludedContainer(name, ctr.Image) {
					continue
				}
				for _, p := range patterns {
					if p.MatchString(name) {
						if ctr.Image != "" {
							authorized[ctr.Image] = true
						}
						break
					}
				}
			}
		}
	}
	return authorized, nil
}

func summarizeVulnerabilities(resultJSON string) (mcpVulnSummary, string) {
	var summary mcpVulnSummary
	var data struct {
		Error   string `json:"Error"`
		Results []struct {
			Vulnerabilities []struct {
				Severity string `json:"Severity"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &data); err != nil {
		return summary, "Invalid scan format"
	}
	if data.Error != "" {
		return summary, data.Error
	}
	for _, target := range data.Results {
		for _, vuln := range target.Vulnerabilities {
			summary.TotalCount++
			switch strings.ToUpper(vuln.Severity) {
			case "CRITICAL":
				summary.Critical++
			case "HIGH":
				summary.High++
			case "MEDIUM":
				summary.Medium++
			case "LOW":
				summary.Low++
			default:
				summary.Unknown++
			}
		}
	}
	return summary, ""
}

func summarizeVulnerabilitiesFromMap(res map[string]interface{}) (mcpVulnSummary, string) {
	b, err := json.Marshal(res)
	if err != nil {
		return mcpVulnSummary{}, err.Error()
	}
	return summarizeVulnerabilities(string(b))
}

// mcpTriggerVulnerabilityScanHandler implements the "trigger_vulnerability_scan" MCP tool.
func mcpTriggerVulnerabilityScanHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isUserScanAuthorized(claims, isAdmin) {
			return mcp.NewToolResultError("Unauthorized: User lacks permission to run vulnerability scans"), nil
		}

		containerID := getMCPStringParam(request, "container_id")
		imageName := getMCPStringParam(request, "image_name")
		wait := getMCPBoolParam(request, "wait")

		if containerID == "" && imageName == "" {
			return mcp.NewToolResultError("Either container_id or image_name must be provided"), nil
		}

		// If container_id is provided, resolve container and check permissions
		if containerID != "" {
			if !isValidContainerID(containerID) {
				return mcp.NewToolResultError("Invalid container ID format"), nil
			}

			if LighthouseMode == "hub" {
				nodeID, containerName, containerImage, remote := cluster.FindSpokeContainer(containerID)
				if remote {
					if inspectContainerExcluded(isAdmin, containerName, containerImage) {
						return mcp.NewToolResultError("Container not found"), nil
					}
					if !isMCPContainerAuthorized(claims.ID, isAdmin, containerName, containerImage) {
						return mcp.NewToolResultError("Unauthorized access to this container"), nil
					}
					if !cluster.SpokeSupports(nodeID, "scan") {
						return mcp.NewToolResultError("Spoke node does not support remote scanning; upgrade required"), nil
					}
					if err := cluster.SendCommandToSpoke(nodeID, "scan", containerID); err != nil {
						return mcp.NewToolResultError(fmt.Sprintf("Failed to dispatch scan to spoke node: %v", err)), nil
					}
					logAudit(claims.ID, claims.Username, "SCAN", "Container:"+containerID, "Success", "Triggered vulnerability scan on Spoke node via MCP")
					return mcp.NewToolResultText(fmt.Sprintf("Vulnerability scan dispatched to remote Spoke node %q for container %q", nodeID, containerID)), nil
				}
			}

			if cli == nil {
				return mcp.NewToolResultError("Docker client unavailable"), nil
			}

			inspectCtx, inspectCancel := context.WithTimeout(ctx, 10*time.Second)
			defer inspectCancel()
			container, err := cli.ContainerInspect(inspectCtx, containerID, client.ContainerInspectOptions{})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Container not found: %v", err)), nil
			}

			containerName := strings.TrimPrefix(container.Container.Name, "/")
			containerImage := ""
			if container.Container.Config != nil {
				containerImage = container.Container.Config.Image
			}

			if !isMCPContainerAuthorized(claims.ID, isAdmin, containerName, containerImage) {
				return mcp.NewToolResultError("Unauthorized access to this container"), nil
			}

			if imageName == "" {
				imageName = containerImage
			}
		}

		if imageName == "" {
			return mcp.NewToolResultError("Could not resolve image name to scan"), nil
		}

		if err := scanner.ValidateImageName(imageName); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid image reference: %v", err)), nil
		}

		// For non-admin without a validated container, check image authorization
		if !isAdmin && containerID == "" {
			authImages, _ := getMCPUserAuthorizedImages(ctx, cli, claims.ID, isAdmin)
			if authImages != nil && !authImages[imageName] {
				return mcp.NewToolResultError(fmt.Sprintf("Unauthorized access to scan image %q: not associated with authorized containers", imageName)), nil
			}
		}

		logAudit(claims.ID, claims.Username, "SCAN", "Image:"+imageName, "Success", fmt.Sprintf("Triggered vulnerability scan for %s (wait=%t) via MCP", imageName, wait))

		if wait {
			scanCtx, scanCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer scanCancel()

			res, err := scanner.ExecuteAndSaveScan(scanCtx, cli, imageName)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Vulnerability scan failed: %v", err)), nil
			}
			summary, errMsg := summarizeVulnerabilitiesFromMap(res)
			if errMsg != "" {
				return mcp.NewToolResultError(fmt.Sprintf("Scan completed with error: %s", errMsg)), nil
			}
			out := map[string]interface{}{
				"status":                "completed",
				"image":                 imageName,
				"total_vulnerabilities": summary.TotalCount,
				"critical":              summary.Critical,
				"high":                  summary.High,
				"medium":                summary.Medium,
				"low":                   summary.Low,
				"message":               fmt.Sprintf("Scan completed for image %s: found %d vulnerabilities (Critical: %d, High: %d, Medium: %d, Low: %d)", imageName, summary.TotalCount, summary.Critical, summary.High, summary.Medium, summary.Low),
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return mcp.NewToolResultText(string(b)), nil
		}

		// Background mode
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			log.Printf("[MCP] Running background scan for image %s...", imageName)
			_, err := scanner.ExecuteAndSaveScan(bgCtx, cli, imageName)
			if err != nil {
				log.Printf("[MCP] Background scan error for %s: %v", imageName, err)
				return
			}
			log.Printf("[MCP] Background scan complete for %s", imageName)
		}()

		return mcp.NewToolResultText(fmt.Sprintf("Vulnerability scan started in background for image %q. Use 'get_vulnerability_scan' or 'list_vulnerability_scans' to view results when finished.", imageName)), nil
	}
}

// mcpListVulnerabilityScansHandler implements the "list_vulnerability_scans" MCP tool.
func mcpListVulnerabilityScansHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isUserScanAuthorized(claims, isAdmin) {
			return mcp.NewToolResultError("Unauthorized: User lacks permission to view vulnerability scans"), nil
		}

		imageFilter := getMCPStringParam(request, "image_name")
		limit := getMCPIntParam(request, "limit", 20)
		if limit <= 0 {
			limit = 20
		} else if limit > 100 {
			limit = 100
		}

		authImages, _ := getMCPUserAuthorizedImages(ctx, cli, claims.ID, isAdmin)

		var scanRecords []db.ImageScanResult
		q := db.GormDB.Order("created_at desc")
		if imageFilter != "" {
			if !isAdmin && authImages != nil && !authImages[imageFilter] {
				return mcp.NewToolResultError(fmt.Sprintf("Unauthorized access to scans for image %q", imageFilter)), nil
			}
			q = q.Where("image = ?", imageFilter)
		}
		if err := q.Limit(limit * 2).Find(&scanRecords).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to query scan results: %v", err)), nil
		}

		type ScanListItem struct {
			ID                   int            `json:"id"`
			Image                string         `json:"image"`
			CreatedAt            string         `json:"created_at"`
			Status               string         `json:"status"`
			Error                string         `json:"error,omitempty"`
			TotalVulnerabilities int            `json:"total_vulnerabilities"`
			SeverityCounts       map[string]int `json:"severity_counts,omitempty"`
		}

		var items []ScanListItem

		for _, rec := range scanRecords {
			if !isAdmin && authImages != nil && !authImages[rec.Image] {
				continue
			}

			summary, errMsg := summarizeVulnerabilities(rec.Result)
			status := "completed"
			if errMsg != "" {
				status = "failed"
			}

			item := ScanListItem{
				ID:                   rec.ID,
				Image:                rec.Image,
				CreatedAt:            rec.CreatedAt.Format(time.RFC3339),
				Status:               status,
				Error:                errMsg,
				TotalVulnerabilities: summary.TotalCount,
			}
			if errMsg == "" {
				item.SeverityCounts = map[string]int{
					"critical": summary.Critical,
					"high":     summary.High,
					"medium":   summary.Medium,
					"low":      summary.Low,
					"unknown":  summary.Unknown,
				}
			}

			items = append(items, item)
			if len(items) >= limit {
				break
			}
		}

		if len(items) == 0 {
			return mcp.NewToolResultText("No vulnerability scans found matching your criteria."), nil
		}

		b, _ := json.MarshalIndent(items, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpGetVulnerabilityScanHandler implements the "get_vulnerability_scan" MCP tool.
func mcpGetVulnerabilityScanHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isUserScanAuthorized(claims, isAdmin) {
			return mcp.NewToolResultError("Unauthorized: User lacks permission to view vulnerability scans"), nil
		}

		imageName := getMCPStringParam(request, "image_name")
		containerID := getMCPStringParam(request, "container_id")
		severityFilter := strings.ToUpper(getMCPStringParam(request, "severity"))

		if imageName == "" && containerID == "" {
			return mcp.NewToolResultError("Either image_name or container_id must be provided"), nil
		}

		if containerID != "" {
			if !isValidContainerID(containerID) {
				return mcp.NewToolResultError("Invalid container ID format"), nil
			}

			if cli == nil {
				return mcp.NewToolResultError("Docker client unavailable"), nil
			}

			inspectCtx, inspectCancel := context.WithTimeout(ctx, 10*time.Second)
			defer inspectCancel()
			container, err := cli.ContainerInspect(inspectCtx, containerID, client.ContainerInspectOptions{})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Container not found: %v", err)), nil
			}

			containerName := strings.TrimPrefix(container.Container.Name, "/")
			containerImage := ""
			if container.Container.Config != nil {
				containerImage = container.Container.Config.Image
			}

			if !isMCPContainerAuthorized(claims.ID, isAdmin, containerName, containerImage) {
				return mcp.NewToolResultError("Unauthorized access to this container"), nil
			}

			if imageName == "" {
				imageName = containerImage
			}
		}

		if !isAdmin && containerID == "" {
			authImages, _ := getMCPUserAuthorizedImages(ctx, cli, claims.ID, isAdmin)
			if authImages != nil && !authImages[imageName] {
				return mcp.NewToolResultError(fmt.Sprintf("Unauthorized access to image %q: not associated with authorized containers", imageName)), nil
			}
		}

		var scan db.ImageScanResult
		if err := db.GormDB.Where("image = ?", imageName).Order("created_at desc").First(&scan).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("No vulnerability scan found for image %q. You can trigger a new scan using trigger_vulnerability_scan.", imageName)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("Failed to query scan results: %v", err)), nil
		}

		// Parse Trivy scan result JSON
		var rawData struct {
			Error   string `json:"Error"`
			Results []struct {
				Target          string `json:"Target"`
				Class           string `json:"Class"`
				Type            string `json:"Type"`
				Vulnerabilities []struct {
					VulnerabilityID  string `json:"VulnerabilityID"`
					PkgName          string `json:"PkgName"`
					InstalledVersion string `json:"InstalledVersion"`
					FixedVersion     string `json:"FixedVersion"`
					Severity         string `json:"Severity"`
					Title            string `json:"Title"`
					Description      string `json:"Description"`
					PrimaryURL       string `json:"PrimaryURL"`
				} `json:"Vulnerabilities"`
			} `json:"Results"`
		}

		if err := json.Unmarshal([]byte(scan.Result), &rawData); err != nil {
			return mcp.NewToolResultError("Invalid or corrupted scan result in database"), nil
		}

		if rawData.Error != "" {
			return mcp.NewToolResultText(fmt.Sprintf("Vulnerability scan for %q (ID: %d, Date: %s) reported an error:\n%s",
				scan.Image, scan.ID, scan.CreatedAt.Format(time.RFC3339), rawData.Error)), nil
		}

		type VulnDetail struct {
			ID               string `json:"vulnerability_id"`
			Package          string `json:"package"`
			InstalledVersion string `json:"installed_version"`
			FixedVersion     string `json:"fixed_version,omitempty"`
			Severity         string `json:"severity"`
			Title            string `json:"title,omitempty"`
			PrimaryURL       string `json:"primary_url,omitempty"`
			Target           string `json:"target,omitempty"`
		}

		var allVulns []VulnDetail
		severityCounts := map[string]int{"CRITICAL": 0, "HIGH": 0, "MEDIUM": 0, "LOW": 0, "UNKNOWN": 0}

		for _, r := range rawData.Results {
			for _, v := range r.Vulnerabilities {
				sev := strings.ToUpper(v.Severity)
				if _, ok := severityCounts[sev]; ok {
					severityCounts[sev]++
				} else {
					severityCounts["UNKNOWN"]++
				}

				if severityFilter != "" && sev != severityFilter {
					continue
				}

				allVulns = append(allVulns, VulnDetail{
					ID:               v.VulnerabilityID,
					Package:          v.PkgName,
					InstalledVersion: v.InstalledVersion,
					FixedVersion:     v.FixedVersion,
					Severity:         v.Severity,
					Title:            v.Title,
					PrimaryURL:       v.PrimaryURL,
					Target:           r.Target,
				})
			}
		}

		report := map[string]interface{}{
			"image":             scan.Image,
			"scan_id":           scan.ID,
			"scanned_at":        scan.CreatedAt.Format(time.RFC3339),
			"severity_counts":   severityCounts,
			"filtered_severity": severityFilter,
			"matching_count":    len(allVulns),
			"vulnerabilities":   allVulns,
		}

		b, _ := json.MarshalIndent(report, "", "  ")
		outStr := string(b)

		// Context window protection: truncate if larger than 50KB
		const maxBytes = 50 * 1024
		if len(outStr) > maxBytes {
			outStr = outStr[:maxBytes] + "\n... [truncated due to output size. Use severity filter 'CRITICAL' or 'HIGH' to narrow results]"
		}

		return mcp.NewToolResultText(outStr), nil
	}
}
