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

	"lighthouse/db"
)

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

	// Create SSE server
	sseServer := server.NewSSEServer(mcpServer, server.WithSSEContextFunc(func(ctx context.Context, req *http.Request) context.Context {
		// Extract claims from the request context if injected
		if claims, ok := req.Context().Value("userClaims").(*UserClaims); ok {
			return context.WithValue(ctx, "userClaims", claims)
		}
		return ctx
	}))

	// Middleware to extract JWT claims and put into http.Request context
	injectClaims := func(next http.Handler) echo.HandlerFunc {
		return func(c echo.Context) error {
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
		if matched, _ := regexp.MatchString(p, containerName); matched {
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

func mcpListContainersHandler(cli *client.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		claims, ok := ctx.Value("userClaims").(*UserClaims)
		if !ok {
			return mcp.NewToolResultError("Unauthorized"), nil
		}

		isAdmin := getMCPUserIsAdmin(claims.ID)
		var patterns []string
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
					if matched, _ := regexp.MatchString(p, name); matched {
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
			return mcp.NewToolResultError("Container not found"), nil
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
			Tail:       "500", // Default to last 500 lines for MCP
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

		result := fmt.Sprintf("=== STDOUT ===\n%s\n=== STDERR ===\n%s", stdout.String(), stderr.String())
		return mcp.NewToolResultText(result), nil
	}
}

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

		b, _ := json.MarshalIndent(container.Container, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	}
}
