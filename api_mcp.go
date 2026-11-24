package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"gorm.io/gorm"

	"lighthouse/cluster"
	"lighthouse/db"
	"lighthouse/scanner"
)

// registerMCPRoutes exposes LightHouse's complete Docker and platform operations
// as MCP (Model Context Protocol) tools over SSE, giving AI agents complete parity
// with every capability in the LightHouse UI. Every tool call is bound to the same
// RBAC, regex patterns, and security toggles as the issuing user's token.
func registerMCPRoutes(r *echo.Group, cli *client.Client) {
	// Create MCP server
	mcpServer := server.NewMCPServer(
		"Lighthouse",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// ─── Container Inspection & Diagnostics ─────────────────────────────
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

	mcpServer.AddTool(mcp.NewTool("get_container_stats",
		mcp.WithDescription("Get real-time resource usage statistics (CPU %, memory, network I/O, disk I/O) for a specific container."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container.")),
	), mcpGetContainerStatsHandler(cli))

	// ─── Container Lifecycle Operations ─────────────────────────────────
	mcpServer.AddTool(mcp.NewTool("start_container",
		mcp.WithDescription("Start a specific container (requires Start permission and ALLOW_START)."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container to start.")),
	), mcpStartContainerHandler(cli))

	mcpServer.AddTool(mcp.NewTool("stop_container",
		mcp.WithDescription("Stop a specific container (requires Stop permission and ALLOW_STOP)."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container to stop.")),
	), mcpStopContainerHandler(cli))

	mcpServer.AddTool(mcp.NewTool("restart_container",
		mcp.WithDescription("Restart a specific container (requires Restart permission and ALLOW_RESTART)."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container to restart.")),
	), mcpRestartContainerHandler(cli))

	mcpServer.AddTool(mcp.NewTool("delete_container",
		mcp.WithDescription("Delete/remove a specific container. Dual-gated by server ALLOW_DELETE and user permissions."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container to delete.")),
		mcp.WithBoolean("force", mcp.Description("Force remove the container even if currently running (default false).")),
	), mcpDeleteContainerHandler(cli))

	mcpServer.AddTool(mcp.NewTool("execute_container_command",
		mcp.WithDescription("Execute a command inside a specific container and return stdout, stderr, and exit code. Dual-gated by server ALLOW_SHELL and user permissions."),
		mcp.WithString("container_id", mcp.Required(), mcp.Description("The ID or name of the container.")),
		mcp.WithString("command", mcp.Required(), mcp.Description("Command string to execute (e.g. 'cat /etc/os-release', 'ls -la /app').")),
		mcp.WithNumber("timeout", mcp.Description("Execution timeout in seconds (default 30, max 120).")),
	), mcpExecuteContainerCommandHandler(cli))

	// ─── Docker Ecosystem & Resources (Admin Only or CanDelete) ─────────
	mcpServer.AddTool(mcp.NewTool("list_images",
		mcp.WithDescription("List available Docker images (Admin only)."),
	), mcpListImagesHandler(cli))

	mcpServer.AddTool(mcp.NewTool("inspect_image",
		mcp.WithDescription("Inspect detailed Docker image metadata including size, OS, architecture, entrypoint, and layers (Admin only)."),
		mcp.WithString("image_name", mcp.Required(), mcp.Description("The Docker image name or ID to inspect.")),
	), mcpInspectImageHandler(cli))

	mcpServer.AddTool(mcp.NewTool("remove_image",
		mcp.WithDescription("Remove/delete a Docker image from the host (Admin or Delete permission required)."),
		mcp.WithString("image_name", mcp.Required(), mcp.Description("The Docker image name or ID to remove.")),
		mcp.WithBoolean("force", mcp.Description("Force removal of the image even if in use (default false).")),
	), mcpRemoveImageHandler(cli))

	mcpServer.AddTool(mcp.NewTool("prune_images",
		mcp.WithDescription("Prune unused or dangling Docker images to reclaim host disk space (Admin or Delete permission required)."),
		mcp.WithBoolean("all_unused", mcp.Description("If true, removes all unused images, not just dangling ones (default false).")),
	), mcpPruneImagesHandler(cli))

	mcpServer.AddTool(mcp.NewTool("list_volumes",
		mcp.WithDescription("List Docker volumes (Admin only)."),
	), mcpListVolumesHandler(cli))

	mcpServer.AddTool(mcp.NewTool("inspect_volume",
		mcp.WithDescription("Inspect detailed metadata for a specific Docker volume (Admin only)."),
		mcp.WithString("volume_name", mcp.Required(), mcp.Description("The name of the volume to inspect.")),
	), mcpInspectVolumeHandler(cli))

	mcpServer.AddTool(mcp.NewTool("prune_volumes",
		mcp.WithDescription("Prune all Docker volumes not currently referenced by any container to reclaim disk space (Admin or Delete permission required)."),
	), mcpPruneVolumesHandler(cli))

	mcpServer.AddTool(mcp.NewTool("list_networks",
		mcp.WithDescription("List Docker networks (Admin only)."),
	), mcpListNetworksHandler(cli))

	mcpServer.AddTool(mcp.NewTool("inspect_network",
		mcp.WithDescription("Inspect detailed metadata, driver configuration, IPAM, and connected containers for a Docker network (Admin only)."),
		mcp.WithString("network_id", mcp.Required(), mcp.Description("The ID or name of the network to inspect.")),
	), mcpInspectNetworkHandler(cli))

	mcpServer.AddTool(mcp.NewTool("prune_networks",
		mcp.WithDescription("Prune all unused Docker networks not currently attached to any container (Admin or Delete permission required)."),
	), mcpPruneNetworksHandler(cli))

	// ─── Vulnerability Scanning Tools ──────────────────────────────────
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

	// ─── GitOps Management ─────────────────────────────────────────────
	mcpServer.AddTool(mcp.NewTool("list_gitops_pipelines",
		mcp.WithDescription("List configured GitOps deployment pipelines, their source types (Git repo or inline Compose), branches, target nodes, and sync statuses."),
	), mcpListGitopsPipelinesHandler())

	mcpServer.AddTool(mcp.NewTool("get_gitops_pipeline",
		mcp.WithDescription("Get detailed configuration for a specific GitOps pipeline by ID."),
		mcp.WithNumber("pipeline_id", mcp.Required(), mcp.Description("The ID of the GitOps pipeline.")),
	), mcpGetGitopsPipelineHandler())

	mcpServer.AddTool(mcp.NewTool("trigger_gitops_deploy",
		mcp.WithDescription("Trigger an immediate GitOps deployment sync for a pipeline without waiting for the polling interval (Admin or Edit Deployments permission required)."),
		mcp.WithNumber("pipeline_id", mcp.Required(), mcp.Description("The ID of the GitOps pipeline to deploy/sync.")),
	), mcpTriggerGitopsDeployHandler())

	mcpServer.AddTool(mcp.NewTool("get_gitops_history",
		mcp.WithDescription("Get historical deployment records, commit SHAs, logs, and statuses for a GitOps pipeline."),
		mcp.WithNumber("pipeline_id", mcp.Required(), mcp.Description("The ID of the GitOps pipeline.")),
		mcp.WithNumber("limit", mcp.Description("Maximum deployment records to return (default 10, max 50).")),
	), mcpGetGitopsHistoryHandler())

	// ─── Fleet & Cluster Nodes ─────────────────────────────────────────
	mcpServer.AddTool(mcp.NewTool("list_cluster_nodes",
		mcp.WithDescription("List all nodes in the LightHouse cluster (Hub and Spokes), their roles, connection statuses, container counts, and supported capabilities."),
	), mcpListClusterNodesHandler())

	mcpServer.AddTool(mcp.NewTool("get_node_details",
		mcp.WithDescription("Get detailed information and connection status for a specific cluster node."),
		mcp.WithString("node_id", mcp.Required(), mcp.Description("The ID of the cluster node (or 'local').")),
	), mcpGetNodeDetailsHandler())

	// ─── Alerts & Incidents ────────────────────────────────────────────
	mcpServer.AddTool(mcp.NewTool("list_alert_rules",
		mcp.WithDescription("List all configured alerting rules, match patterns, event types, metric thresholds, notification channels, and active states (Admin only)."),
	), mcpListAlertRulesHandler())

	mcpServer.AddTool(mcp.NewTool("get_alert_history",
		mcp.WithDescription("Get fired alert history incidents and delivery statuses across the cluster."),
		mcp.WithNumber("limit", mcp.Description("Maximum incident records to return (default 20, max 100).")),
		mcp.WithString("rule_name", mcp.Description("Optional alert rule name filter.")),
		mcp.WithString("container_name", mcp.Description("Optional container name filter.")),
	), mcpGetAlertHistoryHandler())

	// ─── Audit & Diagnostics ───────────────────────────────────────────
	mcpServer.AddTool(mcp.NewTool("get_audit_logs",
		mcp.WithDescription("Query recent security and administrative audit logs recording actions across the cluster (Admin only)."),
		mcp.WithNumber("limit", mcp.Description("Maximum audit records to return (default 50, max 200).")),
		mcp.WithString("action", mcp.Description("Optional action filter (e.g. 'START', 'STOP', 'DELETE', 'SCAN', 'EXEC').")),
		mcp.WithString("username", mcp.Description("Optional username filter.")),
	), mcpGetAuditLogsHandler())

	mcpServer.AddTool(mcp.NewTool("get_system_info",
		mcp.WithDescription("Get host system diagnostic information, Docker engine version, Docker Compose version, CPU/Memory stats snapshot, and polling health."),
	), mcpGetSystemInfoHandler(cli))

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
		if !isAdmin && !containerActionEnvAllowed("start") {
			return mcp.NewToolResultError("Start action is disabled on this server"), nil
		}
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
		if !isAdmin && !containerActionEnvAllowed("stop") {
			return mcp.NewToolResultError("Stop action is disabled on this server"), nil
		}
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
		if isLightHouseSelfContainer(container.Container.Name, image) {
			return mcp.NewToolResultError("Cannot stop the LightHouse platform container"), nil
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
		if !isAdmin && !containerActionEnvAllowed("restart") {
			return mcp.NewToolResultError("Restart action is disabled on this server"), nil
		}
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

// mcpDeleteContainerHandler implements the "delete_container" MCP tool.
func mcpDeleteContainerHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !containerActionEnvAllowed("remove") {
			return mcp.NewToolResultError("Delete action is disabled on this server"), nil
		}
		if !isAdmin && !claims.CanDelete {
			return mcp.NewToolResultError("Unauthorized: User lacks Delete permission"), nil
		}

		containerID := getMCPStringParam(request, "container_id")
		if containerID == "" {
			var err error
			containerID, err = request.RequireString("container_id")
			if err != nil {
				return mcp.NewToolResultError("container_id is required"), nil
			}
		}

		if !isValidContainerID(containerID) {
			return mcp.NewToolResultError("Invalid container ID format"), nil
		}

		force := getMCPBoolParam(request, "force")

		// Spoke check in hub mode
		if LighthouseMode == "hub" {
			nodeID, targetName, targetImage, remote := cluster.FindSpokeContainer(containerID)
			if remote {
				if isLightHouseSelfContainer(targetName, targetImage) {
					return mcp.NewToolResultError("Cannot delete the LightHouse platform container"), nil
				}
				if !isMCPContainerAuthorized(claims.ID, isAdmin, targetName, targetImage) {
					return mcp.NewToolResultError("Unauthorized access to this container"), nil
				}
				if !cluster.SpokeSupports(nodeID, "actions") {
					return mcp.NewToolResultError("Spoke upgrade required for remote actions"), nil
				}
				actionCtx, actionCancel := context.WithTimeout(ctx, 30*time.Second)
				defer actionCancel()
				_, err := cluster.CallSpoke(actionCtx, nodeID, "container_action", map[string]string{"container_id": containerID, "action": "remove"})
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Failed to remove container on spoke: %v", err)), nil
				}
				logAudit(claims.ID, claims.Username, "DELETE", containerID, "Success", "Container removed on spoke "+nodeID+" via MCP")
				return mcp.NewToolResultText(fmt.Sprintf("Container %s removed successfully on node %s", containerID, nodeID)), nil
			}
		}

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		inspectCtx, inspectCancel := context.WithTimeout(ctx, 10*time.Second)
		defer inspectCancel()
		container, err := cli.ContainerInspect(inspectCtx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("Container not found"), nil
		}

		image := ""
		if container.Container.Config != nil {
			image = container.Container.Config.Image
		}
		name := strings.TrimPrefix(container.Container.Name, "/")
		if isLightHouseSelfContainer(name, image) {
			return mcp.NewToolResultError("Cannot delete the LightHouse platform container"), nil
		}
		if !isMCPContainerAuthorized(claims.ID, isAdmin, name, image) {
			return mcp.NewToolResultError("Unauthorized to delete this container"), nil
		}

		removeCtx, removeCancel := context.WithTimeout(ctx, 30*time.Second)
		defer removeCancel()
		if _, err := cli.ContainerRemove(removeCtx, container.Container.ID, client.ContainerRemoveOptions{Force: force}); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to delete container: %v", err)), nil
		}

		logAudit(claims.ID, claims.Username, "DELETE", container.Container.ID, "Success", "Container removed via MCP")
		return mcp.NewToolResultText("Container deleted successfully"), nil
	}
}

// mcpGetContainerStatsHandler implements the "get_container_stats" MCP tool.
func mcpGetContainerStatsHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		containerID := getMCPStringParam(request, "container_id")
		if containerID == "" {
			var err error
			containerID, err = request.RequireString("container_id")
			if err != nil {
				return mcp.NewToolResultError("container_id is required"), nil
			}
		}

		if !isValidContainerID(containerID) {
			return mcp.NewToolResultError("Invalid container ID format"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		inspectCtx, inspectCancel := context.WithTimeout(ctx, 10*time.Second)
		defer inspectCancel()
		container, err := cli.ContainerInspect(inspectCtx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("Container not found"), nil
		}

		image := ""
		if container.Container.Config != nil {
			image = container.Container.Config.Image
		}
		name := strings.TrimPrefix(container.Container.Name, "/")
		if !isMCPContainerAuthorized(claims.ID, isAdmin, name, image) {
			return mcp.NewToolResultError("Unauthorized access to this container"), nil
		}

		statsCtx, statsCancel := context.WithTimeout(ctx, 5*time.Second)
		defer statsCancel()
		s, err := cli.ContainerStats(statsCtx, container.Container.ID, client.ContainerStatsOptions{Stream: false})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to fetch container stats: %v", err)), nil
		}
		defer s.Body.Close()

		var rawStats struct {
			Read    string `json:"read"`
			Preread string `json:"preread"`
			CPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemUsage uint64 `json:"system_cpu_usage"`
				OnlineCPUs  uint32 `json:"online_cpus"`
			} `json:"cpu_stats"`
			PreCPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemUsage uint64 `json:"system_cpu_usage"`
			} `json:"precpu_stats"`
			MemoryStats struct {
				Usage uint64 `json:"usage"`
				Limit uint64 `json:"limit"`
			} `json:"memory_stats"`
			Networks map[string]struct {
				RxBytes uint64 `json:"rx_bytes"`
				TxBytes uint64 `json:"tx_bytes"`
			} `json:"networks"`
			BlkioStats struct {
				IoServiceBytesRecursive []struct {
					Op    string `json:"op"`
					Value uint64 `json:"value"`
				} `json:"io_service_bytes_recursive"`
			} `json:"blkio_stats"`
		}

		if err := json.NewDecoder(s.Body).Decode(&rawStats); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to decode container stats: %v", err)), nil
		}

		cpuPercent := 0.0
		curTotal := rawStats.CPUStats.CPUUsage.TotalUsage
		curSys := rawStats.CPUStats.SystemUsage
		prevTotal := rawStats.PreCPUStats.CPUUsage.TotalUsage
		prevSys := rawStats.PreCPUStats.SystemUsage
		if curSys > prevSys && curTotal >= prevTotal {
			cpuDelta := float64(curTotal - prevTotal)
			sysDelta := float64(curSys - prevSys)
			cpus := float64(rawStats.CPUStats.OnlineCPUs)
			if cpus == 0 {
				cpus = float64(runtime.NumCPU())
			}
			cpuPercent = (cpuDelta / sysDelta) * cpus * 100.0
		}

		var totalRx, totalTx uint64
		for _, net := range rawStats.Networks {
			totalRx += net.RxBytes
			totalTx += net.TxBytes
		}

		var blkRead, blkWrite uint64
		for _, io := range rawStats.BlkioStats.IoServiceBytesRecursive {
			switch strings.ToLower(io.Op) {
			case "read":
				blkRead += io.Value
			case "write":
				blkWrite += io.Value
			}
		}

		memPercent := 0.0
		if rawStats.MemoryStats.Limit > 0 {
			memPercent = (float64(rawStats.MemoryStats.Usage) / float64(rawStats.MemoryStats.Limit)) * 100.0
		}

		result := map[string]interface{}{
			"container_id":       container.Container.ID,
			"name":               name,
			"cpu_percent":        fmt.Sprintf("%.2f%%", cpuPercent),
			"memory_usage_bytes": rawStats.MemoryStats.Usage,
			"memory_limit_bytes": rawStats.MemoryStats.Limit,
			"memory_percent":     fmt.Sprintf("%.2f%%", memPercent),
			"network_rx_bytes":   totalRx,
			"network_tx_bytes":   totalTx,
			"block_read_bytes":   blkRead,
			"block_write_bytes":  blkWrite,
			"read_time":          rawStats.Read,
		}

		b, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpExecCommandRunner defines the function used to execute a non-interactive command inside a container.
// It is exposed as a package variable to allow mocking in unit tests without requiring a real Docker daemon socket hijack.
var mcpExecCommandRunner = func(ctx context.Context, cli *client.Client, containerID string, command string) (int, string, string, error) {
	execConfig := client.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"sh", "-c", command},
	}

	execResult, err := cli.ExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to create exec session: %w", err)
	}

	attachResp, err := cli.ExecAttach(ctx, execResult.ID, client.ExecAttachOptions{})
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to attach to exec session: %w", err)
	}
	defer attachResp.Close()

	stdout := new(strings.Builder)
	stderr := new(strings.Builder)
	_, err = stdcopy.StdCopy(stdout, stderr, attachResp.Reader)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		log.Printf("[MCP] StdCopy error during command exec: %v", err)
	}

	exitCode := 0
	inspectResult, err := cli.ExecInspect(ctx, execResult.ID, client.ExecInspectOptions{})
	if err == nil {
		exitCode = inspectResult.ExitCode
	}

	return exitCode, stdout.String(), stderr.String(), nil
}

// mcpExecuteContainerCommandHandler implements the "execute_container_command" MCP tool.
func mcpExecuteContainerCommandHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		if !AllowShell {
			return mcp.NewToolResultError("Shell execution is disabled on this server (ALLOW_SHELL is false)"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanShell {
			return mcp.NewToolResultError("Unauthorized: User lacks Shell execution permission"), nil
		}

		containerID := getMCPStringParam(request, "container_id")
		if containerID == "" {
			var err error
			containerID, err = request.RequireString("container_id")
			if err != nil {
				return mcp.NewToolResultError("container_id is required"), nil
			}
		}

		if !isValidContainerID(containerID) {
			return mcp.NewToolResultError("Invalid container ID format"), nil
		}

		command := getMCPStringParam(request, "command")
		if command == "" {
			var err error
			command, err = request.RequireString("command")
			if err != nil {
				return mcp.NewToolResultError("command is required"), nil
			}
		}

		timeout := getMCPIntParam(request, "timeout", 30)
		if timeout <= 0 {
			timeout = 30
		} else if timeout > 120 {
			timeout = 120
		}

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		inspectCtx, inspectCancel := context.WithTimeout(ctx, 10*time.Second)
		defer inspectCancel()
		container, err := cli.ContainerInspect(inspectCtx, containerID, client.ContainerInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError("Container not found"), nil
		}

		image := ""
		if container.Container.Config != nil {
			image = container.Container.Config.Image
		}
		name := strings.TrimPrefix(container.Container.Name, "/")
		if isLightHouseSelfContainer(name, image) {
			return mcp.NewToolResultError("Cannot execute commands inside the LightHouse platform container"), nil
		}
		if !isMCPContainerAuthorized(claims.ID, isAdmin, name, image) {
			return mcp.NewToolResultError("Unauthorized access to this container"), nil
		}

		execCtx, execCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer execCancel()

		exitCode, stdoutStr, stderrStr, err := mcpExecCommandRunner(execCtx, cli, container.Container.ID, command)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Command execution failed: %v", err)), nil
		}

		logAudit(claims.ID, claims.Username, "EXEC", container.Container.ID, "Success", fmt.Sprintf("Executed command via MCP: %s", command))

		const maxOutput = 50 * 1024
		if len(stdoutStr) > maxOutput {
			stdoutStr = stdoutStr[:maxOutput] + "\n... [stdout truncated]"
		}
		if len(stderrStr) > maxOutput {
			stderrStr = stderrStr[:maxOutput] + "\n... [stderr truncated]"
		}

		result := map[string]interface{}{
			"container_id": container.Container.ID,
			"command":      command,
			"exit_code":    exitCode,
			"stdout":       stdoutStr,
			"stderr":       stderrStr,
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
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

// mcpInspectImageHandler implements the admin-only "inspect_image" MCP tool.
func mcpInspectImageHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok || !getMCPUserIsAdmin(claims.ID) {
			return mcp.NewToolResultError("Unauthorized: Admin only"), nil
		}

		imageName := getMCPStringParam(request, "image_name")
		if imageName == "" {
			var err error
			imageName, err = request.RequireString("image_name")
			if err != nil {
				return mcp.NewToolResultError("image_name is required"), nil
			}
		}

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		inspect, err := cli.ImageInspect(inspectCtx, imageName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Image not found: %v", err)), nil
		}

		b, _ := json.MarshalIndent(inspect, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpRemoveImageHandler implements the "remove_image" MCP tool.
func mcpRemoveImageHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanDelete {
			return mcp.NewToolResultError("Unauthorized: User lacks Delete permission"), nil
		}

		imageName := getMCPStringParam(request, "image_name")
		if imageName == "" {
			var err error
			imageName, err = request.RequireString("image_name")
			if err != nil {
				return mcp.NewToolResultError("image_name is required"), nil
			}
		}

		force := getMCPBoolParam(request, "force")

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		removeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		res, err := cli.ImageRemove(removeCtx, imageName, client.ImageRemoveOptions{Force: force, PruneChildren: true})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to remove image: %v", err)), nil
		}

		logAudit(claims.ID, claims.Username, "DELETE", "Image:"+imageName, "Success", "Image removed via MCP")
		b, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("Image %s removed successfully:\n%s", imageName, string(b))), nil
	}
}

// mcpPruneImagesHandler implements the "prune_images" MCP tool.
func mcpPruneImagesHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanDelete {
			return mcp.NewToolResultError("Unauthorized: User lacks Delete/Prune permission"), nil
		}

		allUnused := getMCPBoolParam(request, "all_unused")

		pruneFilters := make(client.Filters)
		if allUnused {
			pruneFilters.Add("dangling", "false")
		} else {
			pruneFilters.Add("dangling", "true")
		}

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		pruneCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		res, err := cli.ImagePrune(pruneCtx, client.ImagePruneOptions{Filters: pruneFilters})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to prune images: %v", err)), nil
		}

		logAudit(claims.ID, claims.Username, "PRUNE", "Images", "Success", "Pruned images via MCP")
		b, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpInspectVolumeHandler implements the admin-only "inspect_volume" MCP tool.
func mcpInspectVolumeHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok || !getMCPUserIsAdmin(claims.ID) {
			return mcp.NewToolResultError("Unauthorized: Admin only"), nil
		}

		volumeName := getMCPStringParam(request, "volume_name")
		if volumeName == "" {
			var err error
			volumeName, err = request.RequireString("volume_name")
			if err != nil {
				return mcp.NewToolResultError("volume_name is required"), nil
			}
		}

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		vol, err := cli.VolumeInspect(inspectCtx, volumeName, client.VolumeInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Volume not found: %v", err)), nil
		}

		b, _ := json.MarshalIndent(vol, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpPruneVolumesHandler implements the "prune_volumes" MCP tool.
func mcpPruneVolumesHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanDelete {
			return mcp.NewToolResultError("Unauthorized: User lacks Delete/Prune permission"), nil
		}

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		pruneCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		res, err := cli.VolumePrune(pruneCtx, client.VolumePruneOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to prune volumes: %v", err)), nil
		}

		logAudit(claims.ID, claims.Username, "PRUNE", "Volumes", "Success", "Pruned volumes via MCP")
		b, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpInspectNetworkHandler implements the admin-only "inspect_network" MCP tool.
func mcpInspectNetworkHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok || !getMCPUserIsAdmin(claims.ID) {
			return mcp.NewToolResultError("Unauthorized: Admin only"), nil
		}

		networkID := getMCPStringParam(request, "network_id")
		if networkID == "" {
			var err error
			networkID, err = request.RequireString("network_id")
			if err != nil {
				return mcp.NewToolResultError("network_id is required"), nil
			}
		}

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		net, err := cli.NetworkInspect(inspectCtx, networkID, client.NetworkInspectOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Network not found: %v", err)), nil
		}

		b, _ := json.MarshalIndent(net, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpPruneNetworksHandler implements the "prune_networks" MCP tool.
func mcpPruneNetworksHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanDelete {
			return mcp.NewToolResultError("Unauthorized: User lacks Delete/Prune permission"), nil
		}

		if cli == nil {
			return mcp.NewToolResultError("Docker client unavailable"), nil
		}

		pruneCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		res, err := cli.NetworkPrune(pruneCtx, client.NetworkPruneOptions{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to prune networks: %v", err)), nil
		}

		logAudit(claims.ID, claims.Username, "PRUNE", "Networks", "Success", "Pruned networks via MCP")
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

func getMCPArguments(req mcp.CallToolRequest) map[string]interface{} {
	if req.Params.Arguments != nil {
		if m, ok := req.Params.Arguments.(map[string]interface{}); ok {
			return m
		}
	}
	if m := req.GetArguments(); m != nil {
		return m
	}
	return nil
}

func getMCPStringParam(req mcp.CallToolRequest, key string) string {
	if s := req.GetString(key, ""); s != "" {
		return strings.TrimSpace(s)
	}
	if args := getMCPArguments(req); args != nil {
		if v, ok := args[key]; ok && v != nil {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func getMCPBoolParam(req mcp.CallToolRequest, key string) bool {
	if req.GetBool(key, false) {
		return true
	}
	if args := getMCPArguments(req); args != nil {
		if v, ok := args[key]; ok && v != nil {
			if b, ok := v.(bool); ok {
				return b
			}
			if s, ok := v.(string); ok {
				return strings.ToLower(strings.TrimSpace(s)) == "true"
			}
		}
	}
	return false
}

func getMCPIntParam(req mcp.CallToolRequest, key string, defaultValue int) int {
	if val := req.GetInt(key, 0); val > 0 {
		return val
	}
	if args := getMCPArguments(req); args != nil {
		if v, ok := args[key]; ok && v != nil {
			switch val := v.(type) {
			case float64:
				if int(val) > 0 {
					return int(val)
				}
			case int:
				if val > 0 {
					return val
				}
			case int64:
				if int(val) > 0 {
					return int(val)
				}
			case string:
				var i int
				if _, err := fmt.Sscanf(strings.TrimSpace(val), "%d", &i); err == nil && i > 0 {
					return i
				}
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

// mcpRunBackgroundScan executes a scan asynchronously. Defined as a package variable so tests can mock or synchronize it.
var mcpRunBackgroundScan = func(cli *client.Client, imageName string) {
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
		mcpRunBackgroundScan(cli, imageName)

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

// ─── GitOps Handlers ────────────────────────────────────────────────────────

// mcpListGitopsPipelinesHandler implements "list_gitops_pipelines"
func mcpListGitopsPipelinesHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		var projects []db.GitProject
		if err := db.GormDB.Find(&projects).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to query GitOps projects: %v", err)), nil
		}

		var patterns []*regexp.Regexp
		if !isAdmin {
			patterns = getAuthorizedPatterns(claims.ID)
		}

		var visible []map[string]interface{}
		for _, p := range projects {
			if !isAdmin {
				allowed := false
				for _, pat := range patterns {
					if pat.MatchString(p.Name) {
						allowed = true
						break
					}
				}
				if !allowed {
					continue
				}
			}

			visible = append(visible, map[string]interface{}{
				"id":           p.ID,
				"name":         p.Name,
				"source_type":  p.SourceType,
				"repo_url":     p.RepoURL,
				"branch":       p.Branch,
				"compose_path": p.ComposePath,
				"target_node":  p.TargetNode,
				"last_commit":  p.LastCommit,
				"status":       p.Status,
				"created_at":   p.CreatedAt.Format(time.RFC3339),
				"updated_at":   p.UpdatedAt.Format(time.RFC3339),
			})
		}

		b, _ := json.MarshalIndent(visible, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpGetGitopsPipelineHandler implements "get_gitops_pipeline"
func mcpGetGitopsPipelineHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		pipelineID := getMCPIntParam(request, "pipeline_id", 0)
		if pipelineID <= 0 {
			return mcp.NewToolResultError("pipeline_id is required"), nil
		}

		var project db.GitProject
		if err := db.GormDB.First(&project, pipelineID).Error; err != nil {
			return mcp.NewToolResultError("GitOps pipeline not found"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin {
			patterns := getAuthorizedPatterns(claims.ID)
			allowed := false
			for _, pat := range patterns {
				if pat.MatchString(project.Name) {
					allowed = true
					break
				}
			}
			if !allowed {
				return mcp.NewToolResultError("Unauthorized access to this GitOps pipeline"), nil
			}
		}

		maskedToken := ""
		if project.AuthToken != "" {
			maskedToken = "***"
		}

		out := map[string]interface{}{
			"id":              project.ID,
			"name":            project.Name,
			"source_type":     project.SourceType,
			"compose_content": project.ComposeContent,
			"repo_url":        project.RepoURL,
			"branch":          project.Branch,
			"compose_path":    project.ComposePath,
			"auth_token":      maskedToken,
			"target_node":     project.TargetNode,
			"last_commit":     project.LastCommit,
			"status":          project.Status,
			"created_at":      project.CreatedAt.Format(time.RFC3339),
			"updated_at":      project.UpdatedAt.Format(time.RFC3339),
		}

		b, _ := json.MarshalIndent(out, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpTriggerGitopsDeployHandler implements "trigger_gitops_deploy"
func mcpTriggerGitopsDeployHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanEditDeployments && !claims.CanCreateDeployments {
			return mcp.NewToolResultError("Unauthorized: User lacks Edit Deployments permission"), nil
		}

		pipelineID := getMCPIntParam(request, "pipeline_id", 0)
		if pipelineID <= 0 {
			return mcp.NewToolResultError("pipeline_id is required"), nil
		}

		var project db.GitProject
		if err := db.GormDB.First(&project, pipelineID).Error; err != nil {
			return mcp.NewToolResultError("GitOps pipeline not found"), nil
		}

		if !isAdmin {
			patterns := getAuthorizedPatterns(claims.ID)
			allowed := false
			for _, pat := range patterns {
				if pat.MatchString(project.Name) {
					allowed = true
					break
				}
			}
			if !allowed {
				return mcp.NewToolResultError("Unauthorized access to this GitOps pipeline"), nil
			}
		}

		if err := db.GormDB.Model(&project).Update("status", "pending").Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to trigger sync: %v", err)), nil
		}

		logAudit(claims.ID, claims.Username, "DEPLOY", fmt.Sprintf("GitOps:%d (%s)", project.ID, project.Name), "Success", "Triggered deployment sync via MCP")
		return mcp.NewToolResultText(fmt.Sprintf("GitOps deployment triggered for pipeline %q (ID: %d). Status set to pending for immediate sync.", project.Name, project.ID)), nil
	}
}

// mcpGetGitopsHistoryHandler implements "get_gitops_history"
func mcpGetGitopsHistoryHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		pipelineID := getMCPIntParam(request, "pipeline_id", 0)
		if pipelineID <= 0 {
			return mcp.NewToolResultError("pipeline_id is required"), nil
		}

		limit := getMCPIntParam(request, "limit", 10)
		if limit <= 0 {
			limit = 10
		} else if limit > 50 {
			limit = 50
		}

		var project db.GitProject
		if err := db.GormDB.First(&project, pipelineID).Error; err != nil {
			return mcp.NewToolResultError("GitOps pipeline not found"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin {
			patterns := getAuthorizedPatterns(claims.ID)
			allowed := false
			for _, pat := range patterns {
				if pat.MatchString(project.Name) {
					allowed = true
					break
				}
			}
			if !allowed {
				return mcp.NewToolResultError("Unauthorized access to this GitOps pipeline"), nil
			}
		}

		var deployments []db.GitDeployment
		if err := db.GormDB.Where("project_id = ?", pipelineID).Order("created_at desc").Limit(limit).Find(&deployments).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to query deployment history: %v", err)), nil
		}

		type DeploymentItem struct {
			ID        int    `json:"id"`
			ProjectID int    `json:"project_id"`
			CommitSHA string `json:"commit_sha"`
			Status    string `json:"status"`
			Logs      string `json:"logs"`
			CreatedAt string `json:"created_at"`
		}

		var items []DeploymentItem
		for _, d := range deployments {
			logs := d.Logs
			if len(logs) > 5000 {
				logs = logs[:5000] + "\n... [truncated]"
			}
			items = append(items, DeploymentItem{
				ID:        d.ID,
				ProjectID: d.ProjectID,
				CommitSHA: d.CommitSHA,
				Status:    d.Status,
				Logs:      logs,
				CreatedAt: d.CreatedAt.Format(time.RFC3339),
			})
		}

		b, _ := json.MarshalIndent(items, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// ─── Cluster & Fleet Nodes Handlers ────────────────────────────────────────

// mcpListClusterNodesHandler implements "list_cluster_nodes"
func mcpListClusterNodesHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanViewSystemHealth {
			return mcp.NewToolResultError("Unauthorized: User lacks View System Health permission"), nil
		}

		globalContainerListMu.RLock()
		localCount := globalContainersCount
		globalContainerListMu.RUnlock()

		nodes := []map[string]interface{}{
			{
				"id":              NodeID,
				"role":            LighthouseMode,
				"connected":       true,
				"last_seen":       time.Now().Format(time.RFC3339),
				"container_count": localCount,
				"capabilities":    []string{"inspect", "logs", "shell", "stats", "actions", "scan", "gitops"},
			},
		}

		if LighthouseMode == "hub" {
			for _, spoke := range cluster.SnapshotSpokes() {
				nodes = append(nodes, map[string]interface{}{
					"id":              spoke.ID,
					"role":            "spoke",
					"connected":       spoke.Connected,
					"last_seen":       spoke.LastSeen.Format(time.RFC3339),
					"container_count": spoke.ContainerCount,
					"capabilities":    spoke.Capabilities,
				})
			}
		}

		b, _ := json.MarshalIndent(nodes, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpGetNodeDetailsHandler implements "get_node_details"
func mcpGetNodeDetailsHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanViewSystemHealth {
			return mcp.NewToolResultError("Unauthorized: User lacks View System Health permission"), nil
		}

		nodeID := getMCPStringParam(request, "node_id")
		if nodeID == "" {
			return mcp.NewToolResultError("node_id is required"), nil
		}

		if nodeID == NodeID || nodeID == "local" {
			globalContainerListMu.RLock()
			localCount := globalContainersCount
			globalContainerListMu.RUnlock()

			nodeInfo := map[string]interface{}{
				"id":              NodeID,
				"role":            LighthouseMode,
				"connected":       true,
				"last_seen":       time.Now().Format(time.RFC3339),
				"container_count": localCount,
				"capabilities":    []string{"inspect", "logs", "shell", "stats", "actions", "scan", "gitops"},
			}
			b, _ := json.MarshalIndent(nodeInfo, "", "  ")
			return mcp.NewToolResultText(string(b)), nil
		}

		if LighthouseMode == "hub" {
			for _, spoke := range cluster.SnapshotSpokes() {
				if spoke.ID == nodeID {
					nodeInfo := map[string]interface{}{
						"id":              spoke.ID,
						"role":            "spoke",
						"connected":       spoke.Connected,
						"last_seen":       spoke.LastSeen.Format(time.RFC3339),
						"container_count": spoke.ContainerCount,
						"capabilities":    spoke.Capabilities,
					}
					b, _ := json.MarshalIndent(nodeInfo, "", "  ")
					return mcp.NewToolResultText(string(b)), nil
				}
			}
		}

		return mcp.NewToolResultError(fmt.Sprintf("Cluster node %q not found", nodeID)), nil
	}
}

// ─── Alerting Handlers ─────────────────────────────────────────────────────

// mcpListAlertRulesHandler implements admin-only "list_alert_rules"
func mcpListAlertRulesHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok || !getMCPUserIsAdmin(claims.ID) {
			return mcp.NewToolResultError("Unauthorized: Admin only"), nil
		}

		var rules []db.AlertRule
		if err := db.GormDB.Find(&rules).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to query alert rules: %v", err)), nil
		}

		b, _ := json.MarshalIndent(rules, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpGetAlertHistoryHandler implements "get_alert_history"
func mcpGetAlertHistoryHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanViewSystemHealth {
			return mcp.NewToolResultError("Unauthorized: User lacks View System Health permission"), nil
		}

		limit := getMCPIntParam(request, "limit", 20)
		if limit <= 0 {
			limit = 20
		} else if limit > 100 {
			limit = 100
		}

		ruleFilter := getMCPStringParam(request, "rule_name")
		containerFilter := getMCPStringParam(request, "container_name")

		q := db.GormDB.Order("timestamp desc")
		if ruleFilter != "" {
			q = q.Where("rule_name = ?", ruleFilter)
		}
		if containerFilter != "" {
			q = q.Where("container_name = ?", containerFilter)
		}

		var history []db.AlertHistory
		if err := q.Limit(limit).Find(&history).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to query alert history: %v", err)), nil
		}

		b, _ := json.MarshalIndent(history, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// ─── Audit & System Diagnostics Handlers ───────────────────────────────────

// mcpGetAuditLogsHandler implements admin-only "get_audit_logs"
func mcpGetAuditLogsHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok || !getMCPUserIsAdmin(claims.ID) {
			return mcp.NewToolResultError("Unauthorized: Admin only"), nil
		}

		limit := getMCPIntParam(request, "limit", 50)
		if limit <= 0 {
			limit = 50
		} else if limit > 200 {
			limit = 200
		}

		actionFilter := strings.ToUpper(getMCPStringParam(request, "action"))
		userFilter := getMCPStringParam(request, "username")

		q := db.GormDB.Order("timestamp desc")
		if actionFilter != "" {
			q = q.Where("action = ?", actionFilter)
		}
		if userFilter != "" {
			q = q.Where("username = ?", userFilter)
		}

		var logs []db.AuditLog
		if err := q.Limit(limit).Find(&logs).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to query audit logs: %v", err)), nil
		}

		b, _ := json.MarshalIndent(logs, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}

// mcpGetSystemInfoHandler implements "get_system_info"
func mcpGetSystemInfoHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		if !isAdmin && !claims.CanViewSystemHealth {
			return mcp.NewToolResultError("Unauthorized: User lacks View System Health permission"), nil
		}

		dockerVer := "Unknown"
		if cli != nil {
			if v, err := cli.ServerVersion(ctx, client.ServerVersionOptions{}); err == nil {
				dockerVer = v.Version
			}
		}

		composeVer := "Unknown"
		if out, err := exec.Command("docker", "compose", "version", "--short").Output(); err == nil {
			composeVer = strings.TrimSpace(string(out))
		} else if out, err = exec.Command("docker-compose", "version", "--short").Output(); err == nil {
			composeVer = strings.TrimSpace(string(out))
		}

		sysStatsMu.RLock()
		snapshot := latestSystemStats
		sysStatsMu.RUnlock()

		info := map[string]interface{}{
			"node_id":         NodeID,
			"mode":            LighthouseMode,
			"docker_version":  dockerVer,
			"compose_version": composeVer,
			"os":              runtime.GOOS,
			"arch":            runtime.GOARCH,
			"cpu_cores":       runtime.NumCPU(),
			"allow_start":     CanStart,
			"allow_stop":      CanStop,
			"allow_restart":   CanRestart,
			"allow_delete":    CanDelete,
			"allow_shell":     AllowShell,
			"system_stats":    snapshot,
		}

		b, _ := json.MarshalIndent(info, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}
