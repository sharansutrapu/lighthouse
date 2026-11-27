package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"lighthouse/db"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// RegisterStackRoutes registers all In-Browser Stack Studio (Docker Compose) routes.
func RegisterStackRoutes(r *echo.Group, cli *client.Client) {
	// GET /stacks - list all stacks and their active status
	r.GET("/stacks", handleGETStacks(cli))

	// GET /stacks/:id - get single stack definition, env, and services
	r.GET("/stacks/:id", handleGETStackByID(cli))

	// POST /stacks - create a new stack definition
	r.POST("/stacks", handlePOSTCreateStack())

	// PUT /stacks/:id - update stack compose/env content
	r.PUT("/stacks/:id", handlePUTUpdateStack())

	// DELETE /stacks/:id - delete stack definition (optional down=true)
	r.DELETE("/stacks/:id", handleDELETEStack(cli))

	// POST /stacks/:id/deploy - deploy stack using `docker compose up -d`
	r.POST("/stacks/:id/deploy", handlePOSTDeployStack(cli))

	// POST /stacks/:id/stop - stop stack containers using `docker compose stop`
	r.POST("/stacks/:id/stop", handlePOSTStopStack(cli))

	// POST /stacks/:id/down - take down stack using `docker compose down`
	r.POST("/stacks/:id/down", handlePOSTDownStack(cli))

	// POST /stacks/:id/restart - restart stack containers
	r.POST("/stacks/:id/restart", handlePOSTRestartStack(cli))

	// POST /stacks/validate - validate compose YAML syntax
	r.POST("/stacks/validate", handlePOSTValidateCompose())
}

// StackResponse enriches db.Stack with live container metrics and counts.
type StackResponse struct {
	db.Stack
	ServiceCount int                      `json:"service_count"`
	RunningCount int                      `json:"running_count"`
	Containers   []map[string]interface{} `json:"containers,omitempty"`
}

func handleGETStacks(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		var stacks []db.Stack
		if err := db.GormDB.Order("updated_at desc").Find(&stacks).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch stacks"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
		defer cancel()

		// Fetch all containers to match compose project labels
		containersRes, _ := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
		containersByProject := make(map[string][]container.Summary)
		for _, ctr := range containersRes.Items {
			if proj, ok := ctr.Labels["com.docker.compose.project"]; ok {
				containersByProject[proj] = append(containersByProject[proj], ctr)
			}
		}

		var responses []StackResponse
		for _, s := range stacks {
			resp := StackResponse{Stack: s}
			if ctrs, ok := containersByProject[s.Name]; ok {
				resp.ServiceCount = len(ctrs)
				running := 0
				for _, ctr := range ctrs {
					if ctr.State == "running" {
						running++
					}
				}
				resp.RunningCount = running
				if running == len(ctrs) && len(ctrs) > 0 {
					resp.Status = "running"
				} else if running > 0 {
					resp.Status = "partial"
				} else {
					resp.Status = "stopped"
				}
			} else {
				resp.Status = "stopped"
			}
			responses = append(responses, resp)
		}

		if responses == nil {
			responses = []StackResponse{}
		}
		return c.JSON(http.StatusOK, responses)
	}
}

func handleGETStackByID(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var stack db.Stack
		if err := db.GormDB.First(&stack, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Stack not found"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
		defer cancel()

		containersRes, _ := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
		var stackContainers []map[string]interface{}
		running := 0
		for _, ctr := range containersRes.Items {
			if ctr.Labels["com.docker.compose.project"] == stack.Name {
				name := ""
				if len(ctr.Names) > 0 {
					name = strings.TrimPrefix(ctr.Names[0], "/")
				}
				stackContainers = append(stackContainers, map[string]interface{}{
					"id":      ctr.ID[:12],
					"name":    name,
					"service": ctr.Labels["com.docker.compose.service"],
					"state":   ctr.State,
					"status":  ctr.Status,
					"image":   ctr.Image,
				})
				if ctr.State == "running" {
					running++
				}
			}
		}

		resp := StackResponse{
			Stack:        stack,
			ServiceCount: len(stackContainers),
			RunningCount: running,
			Containers:   stackContainers,
		}
		return c.JSON(http.StatusOK, resp)
	}
}

func handlePOSTCreateStack() echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanCreateDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		var req struct {
			Name           string `json:"name"`
			ComposeContent string `json:"compose_content"`
			EnvContent     string `json:"env_content"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		}

		req.Name = strings.TrimSpace(strings.ToLower(req.Name))
		if req.Name == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Stack name is required"})
		}
		if req.ComposeContent == "" {
			req.ComposeContent = "version: '3.8'\nservices:\n"
		}

		stack := db.Stack{
			Name:           req.Name,
			ComposeContent: req.ComposeContent,
			EnvContent:     req.EnvContent,
			Status:         "stopped",
		}
		if err := db.GormDB.Create(&stack).Error; err != nil {
			return c.JSON(http.StatusConflict, map[string]string{"error": "Stack with this name already exists"})
		}

		logAudit(userClaims.ID, userClaims.Username, "CREATE", "Stack:"+req.Name, "Success", "Created stack definition")
		return c.JSON(http.StatusCreated, stack)
	}
}

func handlePUTUpdateStack() echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanEditDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		id := c.Param("id")
		var stack db.Stack
		if err := db.GormDB.First(&stack, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Stack not found"})
		}

		var req struct {
			ComposeContent string `json:"compose_content"`
			EnvContent     string `json:"env_content"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		}

		stack.ComposeContent = req.ComposeContent
		stack.EnvContent = req.EnvContent
		stack.UpdatedAt = time.Now()
		if err := db.GormDB.Save(&stack).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update stack"})
		}

		logAudit(userClaims.ID, userClaims.Username, "UPDATE", "Stack:"+stack.Name, "Success", "Updated stack compose/env content")
		return c.JSON(http.StatusOK, stack)
	}
}

func handleDELETEStack(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDeleteDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		id := c.Param("id")
		var stack db.Stack
		if err := db.GormDB.First(&stack, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Stack not found"})
		}

		if c.QueryParam("down") == "true" {
			_, _ = runComposeCommand(stack.Name, stack.ComposeContent, stack.EnvContent, "down", "--volumes")
		}

		if err := db.GormDB.Delete(&stack).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete stack"})
		}

		logAudit(userClaims.ID, userClaims.Username, "DELETE", "Stack:"+stack.Name, "Success", "Deleted stack")
		return c.JSON(http.StatusOK, map[string]string{"message": "Stack deleted successfully"})
	}
}

func handlePOSTDeployStack(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanCreateDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		if !CanStart {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Host server disabled ALLOW_START flag"})
		}

		id := c.Param("id")
		var stack db.Stack
		if err := db.GormDB.First(&stack, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Stack not found"})
		}

		out, err := runComposeCommand(stack.Name, stack.ComposeContent, stack.EnvContent, "up", "-d", "--remove-orphans")
		if err != nil {
			db.GormDB.Model(&stack).Update("status", "failed")
			logAudit(userClaims.ID, userClaims.Username, "DEPLOY_FAILED", "Stack:"+stack.Name, "Failed", string(out))
			return c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"error":  "Failed to deploy stack",
				"output": string(out),
			})
		}

		db.GormDB.Model(&stack).Update("status", "running")
		logAudit(userClaims.ID, userClaims.Username, "DEPLOY", "Stack:"+stack.Name, "Success", "Stack deployed successfully")
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message": "Stack deployed successfully",
			"output":  string(out),
		})
	}
}

func handlePOSTStopStack(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanStop {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		id := c.Param("id")
		var stack db.Stack
		if err := db.GormDB.First(&stack, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Stack not found"})
		}

		out, err := runComposeCommand(stack.Name, stack.ComposeContent, stack.EnvContent, "stop")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"error":  "Failed to stop stack",
				"output": string(out),
			})
		}

		db.GormDB.Model(&stack).Update("status", "stopped")
		logAudit(userClaims.ID, userClaims.Username, "STOP", "Stack:"+stack.Name, "Success", "Stack stopped")
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message": "Stack stopped",
			"output":  string(out),
		})
	}
}

func handlePOSTDownStack(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanDeleteDeployments {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		id := c.Param("id")
		var stack db.Stack
		if err := db.GormDB.First(&stack, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Stack not found"})
		}

		out, err := runComposeCommand(stack.Name, stack.ComposeContent, stack.EnvContent, "down")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"error":  "Failed to take down stack",
				"output": string(out),
			})
		}

		db.GormDB.Model(&stack).Update("status", "stopped")
		logAudit(userClaims.ID, userClaims.Username, "DOWN", "Stack:"+stack.Name, "Success", "Stack taken down")
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message": "Stack taken down",
			"output":  string(out),
		})
	}
}

func handlePOSTRestartStack(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanRestart {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		id := c.Param("id")
		var stack db.Stack
		if err := db.GormDB.First(&stack, id).Error; err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Stack not found"})
		}

		out, err := runComposeCommand(stack.Name, stack.ComposeContent, stack.EnvContent, "restart")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"error":  "Failed to restart stack",
				"output": string(out),
			})
		}

		db.GormDB.Model(&stack).Update("status", "running")
		logAudit(userClaims.ID, userClaims.Username, "RESTART", "Stack:"+stack.Name, "Success", "Stack restarted")
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message": "Stack restarted",
			"output":  string(out),
		})
	}
}

func handlePOSTValidateCompose() echo.HandlerFunc {
	return func(c echo.Context) error {
		var req struct {
			ComposeContent string `json:"compose_content"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
		}

		tmpDir, err := os.MkdirTemp("", "lh-validate-*")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create temp validation dir"})
		}
		defer os.RemoveAll(tmpDir)

		composePath := filepath.Join(tmpDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, []byte(req.ComposeContent), 0644); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to write temp compose file"})
		}

		cmd := exec.Command("docker", "compose", "-f", composePath, "config", "-q")
		out, err := cmd.CombinedOutput()
		if err != nil {
			// fallback check with docker-compose
			cmd2 := exec.Command("docker-compose", "-f", composePath, "config", "-q")
			out2, err2 := cmd2.CombinedOutput()
			if err2 != nil {
				return c.JSON(http.StatusBadRequest, map[string]interface{}{
					"valid": false,
					"error": strings.TrimSpace(string(out) + "\n" + string(out2)),
				})
			}
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"valid":   true,
			"message": "Docker Compose YAML is valid",
		})
	}
}

// runComposeCommand stages compose & env content into an isolated stack directory and executes docker compose.
func runComposeCommand(projectName, composeYAML, envContent string, args ...string) ([]byte, error) {
	stackDir := filepath.Join(os.TempDir(), "lighthouse-stacks", projectName)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stack workspace: %w", err)
	}

	composeFile := filepath.Join(stackDir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte(composeYAML), 0644); err != nil {
		return nil, fmt.Errorf("failed to write compose file: %w", err)
	}

	if envContent != "" {
		envFile := filepath.Join(stackDir, ".env")
		_ = os.WriteFile(envFile, []byte(envContent), 0644)
	}

	cmdArgs := append([]string{"compose", "-p", projectName, "-f", composeFile}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Dir = stackDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try legacy docker-compose syntax
		cmdArgsLegacy := append([]string{"-p", projectName, "-f", composeFile}, args...)
		cmdLegacy := exec.Command("docker-compose", cmdArgsLegacy...)
		cmdLegacy.Dir = stackDir
		outLegacy, errLegacy := cmdLegacy.CombinedOutput()
		if errLegacy == nil {
			return outLegacy, nil
		}
		log.Printf("[Stack Studio] Command error for project %s: %s", projectName, string(out))
		return out, err
	}
	return out, nil
}
