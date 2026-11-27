package main

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// RegisterContainerExtraRoutes registers Container Creation Wizard,
// Dynamic Resource Tuning, and In-Browser File Explorer endpoints.
func RegisterContainerExtraRoutes(r *echo.Group, cli *client.Client) {
	// 1. Container Creation Wizard
	r.POST("/containers/create", handlePOSTContainersCreate(cli))

	// 2. Dynamic Resource Tuning (docker update)
	r.POST("/containers/:id/update", handlePOSTContainersUpdate(cli))

	// 3. In-Browser File Explorer for Containers
	r.GET("/containers/:id/files", handleGETContainerFiles(cli))
	r.GET("/containers/:id/files/content", handleGETContainerFileContent(cli))
	r.PUT("/containers/:id/files/content", handlePUTContainerFileContent(cli))
	r.GET("/containers/:id/files/download", handleGETContainerFileDownload(cli))
	r.POST("/containers/:id/files/upload", handlePOSTContainerFileUpload(cli))
}

// ─── 1. Container Creation Wizard ─────────────────────────────────────────────

type ContainerCreateRequest struct {
	Name          string   `json:"name"`
	Image         string   `json:"image"`
	Command       string   `json:"command"`
	Ports         []string `json:"ports"`          // e.g. ["8080:80", "3000:3000/tcp"]
	Env           []string `json:"env"`            // e.g. ["PORT=3000", "ENV=prod"]
	Volumes       []string `json:"volumes"`        // e.g. ["my-data:/data", "/host/path:/container/path:rw"]
	RestartPolicy string   `json:"restart_policy"` // "no", "always", "unless-stopped", "on-failure"
	Network       string   `json:"network"`
	MemoryMB      int64    `json:"memory_mb"`
	CPUShares     int64    `json:"cpu_shares"`
	Start         bool     `json:"start"`
}

func handlePOSTContainersCreate(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanCreateDeployments && !userClaims.CanStart {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		if !CanStart {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Host server disabled ALLOW_START flag"})
		}

		var req ContainerCreateRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid container payload"})
		}

		req.Name = strings.TrimSpace(req.Name)
		req.Image = strings.TrimSpace(req.Image)
		if req.Image == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Container image is required"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 60*time.Second)
		defer cancel()

		// Prepare container config
		cfg := &container.Config{
			Image: req.Image,
			Env:   req.Env,
		}

		if strings.TrimSpace(req.Command) != "" {
			cfg.Cmd = strings.Fields(req.Command)
		}

		// Configure Port Bindings
		exposedPorts := make(network.PortSet)
		portBindings := make(network.PortMap)
		for _, p := range req.Ports {
			parts := strings.Split(p, ":")
			if len(parts) == 2 {
				hostPort := parts[0]
				ctrPortStr := parts[1]
				proto := "tcp"
				if strings.Contains(ctrPortStr, "/") {
					sub := strings.Split(ctrPortStr, "/")
					ctrPortStr = sub[0]
					proto = sub[1]
				}
				if portKey, err := network.ParsePort(fmt.Sprintf("%s/%s", ctrPortStr, proto)); err == nil {
					exposedPorts[portKey] = struct{}{}
					portBindings[portKey] = append(portBindings[portKey], network.PortBinding{
						HostPort: hostPort,
					})
				}
			} else if len(parts) == 1 && parts[0] != "" {
				if portKey, err := network.ParsePort(parts[0] + "/tcp"); err == nil {
					exposedPorts[portKey] = struct{}{}
				}
			}
		}
		cfg.ExposedPorts = exposedPorts

		// Configure HostConfig
		hostConfig := &container.HostConfig{
			PortBindings: portBindings,
			Binds:        req.Volumes,
		}

		if req.RestartPolicy != "" {
			hostConfig.RestartPolicy = container.RestartPolicy{
				Name: container.RestartPolicyMode(req.RestartPolicy),
			}
		} else {
			hostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}
		}

		if req.MemoryMB > 0 {
			hostConfig.Memory = req.MemoryMB * 1024 * 1024
		}
		if req.CPUShares > 0 {
			hostConfig.CPUShares = req.CPUShares
		}

		networkingConfig := &network.NetworkingConfig{}
		if req.Network != "" && req.Network != "default" {
			hostConfig.NetworkMode = container.NetworkMode(req.Network)
		}

		createOpts := client.ContainerCreateOptions{
			Config:           cfg,
			HostConfig:       hostConfig,
			NetworkingConfig: networkingConfig,
			Name:             req.Name,
		}

		createResp, err := cli.ContainerCreate(ctx, createOpts)
		if err != nil {
			// If image not found locally, try to pull it first
			if strings.Contains(err.Error(), "No such image") || strings.Contains(err.Error(), "not found") {
				reader, pullErr := cli.ImagePull(ctx, req.Image, client.ImagePullOptions{})
				if pullErr == nil {
					_, _ = io.Copy(io.Discard, reader)
					_ = reader.Close()
					createResp, err = cli.ContainerCreate(ctx, createOpts)
				}
			}
		}

		if err != nil {
			logAudit(userClaims.ID, userClaims.Username, "CREATE_FAILED", "Container:"+req.Name, "Failed", err.Error())
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to create container: %v", err)})
		}

		// Optionally auto-start
		if req.Start {
			if _, startErr := cli.ContainerStart(ctx, createResp.ID, client.ContainerStartOptions{}); startErr != nil {
				logAudit(userClaims.ID, userClaims.Username, "START_FAILED", "Container:"+createResp.ID[:12], "Failed", startErr.Error())
				return c.JSON(http.StatusCreated, map[string]interface{}{
					"id":       createResp.ID,
					"warnings": createResp.Warnings,
					"started":  false,
					"error":    fmt.Sprintf("Created, but failed to start: %v", startErr),
				})
			}
		}

		logAudit(userClaims.ID, userClaims.Username, "CREATE", "Container:"+createResp.ID[:12], "Success", fmt.Sprintf("Created container %s with image %s", req.Name, req.Image))
		return c.JSON(http.StatusCreated, map[string]interface{}{
			"id":       createResp.ID,
			"warnings": createResp.Warnings,
			"started":  req.Start,
		})
	}
}

// ─── 2. Dynamic Resource Tuning (docker update) ────────────────────────────────

type ContainerUpdateRequest struct {
	MemoryMB      int64  `json:"memory_mb"`
	MemorySwapMB  int64  `json:"memory_swap_mb"`
	CPUShares     int64  `json:"cpu_shares"`
	NanoCPUs      int64  `json:"nano_cpus"`
	RestartPolicy string `json:"restart_policy"`
}

func handlePOSTContainersUpdate(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanStart {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		id := c.Param("id")
		var req ContainerUpdateRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid update payload"})
		}

		resConfig := container.Resources{}
		if req.MemoryMB > 0 {
			resConfig.Memory = req.MemoryMB * 1024 * 1024
		}
		if req.MemorySwapMB > 0 {
			resConfig.MemorySwap = req.MemorySwapMB * 1024 * 1024
		}
		if req.CPUShares > 0 {
			resConfig.CPUShares = req.CPUShares
		}
		if req.NanoCPUs > 0 {
			resConfig.NanoCPUs = req.NanoCPUs
		}

		updateOpts := client.ContainerUpdateOptions{
			Resources: &resConfig,
		}
		if req.RestartPolicy != "" {
			updateOpts.RestartPolicy = &container.RestartPolicy{
				Name: container.RestartPolicyMode(req.RestartPolicy),
			}
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
		defer cancel()

		resp, err := cli.ContainerUpdate(ctx, id, updateOpts)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to update container: %v", err)})
		}

		logAudit(userClaims.ID, userClaims.Username, "RESOURCE_TUNING", "Container:"+id, "Success", fmt.Sprintf("Updated limits: mem=%dMB, cpu_shares=%d", req.MemoryMB, req.CPUShares))
		return c.JSON(http.StatusOK, map[string]interface{}{
			"message":  "Container resources updated successfully",
			"warnings": resp.Warnings,
		})
	}
}

// ─── 3. In-Browser Container File Explorer ────────────────────────────────────

type FileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	IsDir   bool   `json:"is_dir"`
	ModTime string `json:"mod_time"`
}

func handleGETContainerFiles(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		reqPath := c.QueryParam("path")
		if reqPath == "" {
			reqPath = "/"
		}
		reqPath = filepath.Clean(reqPath)

		ctx, cancel := context.WithTimeout(c.Request().Context(), 15*time.Second)
		defer cancel()

		// Execute ls -lap in container to get directory entries cleanly
		cmd := []string{"sh", "-c", fmt.Sprintf("ls -lap %s 2>/dev/null || ls -la %s", reqPath, reqPath)}
		execConfig := client.ExecCreateOptions{
			Cmd:          cmd,
			AttachStdout: true,
			AttachStderr: true,
		}
		execResult, err := cli.ExecCreate(ctx, id, execConfig)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to inspect container path"})
		}

		attachResp, err := cli.ExecAttach(ctx, execResult.ID, client.ExecAttachOptions{})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to read path entries"})
		}
		defer attachResp.Close()

		var outBuf bytes.Buffer
		_, _ = io.Copy(&outBuf, attachResp.Reader)

		lines := strings.Split(outBuf.String(), "\n")
		var entries []FileEntry

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "total") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 9 {
				continue
			}
			name := strings.Join(parts[8:], " ")
			if name == "." || name == ".." || name == "./" || name == "../" {
				continue
			}
			isDir := strings.HasPrefix(parts[0], "d") || strings.HasSuffix(name, "/")
			name = strings.TrimSuffix(name, "/")

			fullPath := filepath.Join(reqPath, name)
			var size int64
			fmt.Sscanf(parts[4], "%d", &size)

			entries = append(entries, FileEntry{
				Name:    name,
				Path:    fullPath,
				Size:    size,
				Mode:    parts[0],
				IsDir:   isDir,
				ModTime: fmt.Sprintf("%s %s %s", parts[5], parts[6], parts[7]),
			})
		}

		if entries == nil {
			entries = []FileEntry{}
		}
		return c.JSON(http.StatusOK, map[string]interface{}{
			"current_path": reqPath,
			"entries":      entries,
		})
	}
}

func handleGETContainerFileContent(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		filePath := c.QueryParam("path")
		if filePath == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "File path parameter required"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 15*time.Second)
		defer cancel()

		result, err := cli.CopyFromContainer(ctx, id, client.CopyFromContainerOptions{
			SourcePath: filePath,
		})
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("File not found or unreadable: %v", err)})
		}
		defer result.Content.Close()

		if result.Stat.Mode.IsDir() {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Target path is a directory, not a file"})
		}

		// Read from tar archive
		tarReader := tar.NewReader(result.Content)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to read tar stream"})
			}

			if !header.FileInfo().IsDir() {
				// Read up to 2MB text content
				const maxTextBytes = 2 * 1024 * 1024
				data, readErr := io.ReadAll(io.LimitReader(tarReader, maxTextBytes))
				if readErr != nil {
					return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed reading file data"})
				}
				return c.JSON(http.StatusOK, map[string]interface{}{
					"path":    filePath,
					"size":    header.Size,
					"content": string(data),
				})
			}
		}

		return c.JSON(http.StatusNotFound, map[string]string{"error": "File not found in archive"})
	}
}

func handlePUTContainerFileContent(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanShell {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		id := c.Param("id")
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := c.Bind(&req); err != nil || req.Path == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Valid file path and content required"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 15*time.Second)
		defer cancel()

		dir := filepath.Dir(req.Path)
		base := filepath.Base(req.Path)

		// Create in-memory tar archive containing the file
		var tarBuf bytes.Buffer
		tw := tar.NewWriter(&tarBuf)

		hdr := &tar.Header{
			Name: base,
			Mode: 0644,
			Size: int64(len(req.Content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create tar header"})
		}
		if _, err := tw.Write([]byte(req.Content)); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to write file bytes to tar"})
		}
		if err := tw.Close(); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed finalizing tar"})
		}

		_, err := cli.CopyToContainer(ctx, id, client.CopyToContainerOptions{
			DestinationPath: dir,
			Content:         &tarBuf,
		})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed copying file to container: %v", err)})
		}

		logAudit(userClaims.ID, userClaims.Username, "FILE_WRITE", "Container:"+id+" File:"+req.Path, "Success", "Updated file content")
		return c.JSON(http.StatusOK, map[string]string{"message": "File saved successfully"})
	}
}

func handleGETContainerFileDownload(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		filePath := c.QueryParam("path")
		if filePath == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "File path parameter required"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 60*time.Second)
		defer cancel()

		result, err := cli.CopyFromContainer(ctx, id, client.CopyFromContainerOptions{
			SourcePath: filePath,
		})
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "File not found or unreadable"})
		}
		defer result.Content.Close()

		baseName := filepath.Base(filePath)
		c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=\"%s.tar\"", baseName))
		c.Response().Header().Set(echo.HeaderContentType, "application/x-tar")
		c.Response().Header().Set("X-File-Size", fmt.Sprintf("%d", result.Stat.Size))
		return c.Stream(http.StatusOK, "application/x-tar", result.Content)
	}
}

func handlePOSTContainerFileUpload(cli *client.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := c.Get("user").(*jwt.Token)
		userClaims := token.Claims.(*UserClaims)
		if !userClaims.IsAdmin && !userClaims.CanShell {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Permission denied"})
		}

		id := c.Param("id")
		destDir := c.FormValue("dir")
		if destDir == "" {
			destDir = "/"
		}

		file, err := c.FormFile("file")
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Uploaded file is required"})
		}

		src, err := file.Open()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed opening uploaded file"})
		}
		defer src.Close()

		var tarBuf bytes.Buffer
		tw := tar.NewWriter(&tarBuf)

		hdr := &tar.Header{
			Name: file.Filename,
			Mode: 0644,
			Size: file.Size,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed generating tar"})
		}
		if _, err := io.Copy(tw, src); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed writing file data"})
		}
		if err := tw.Close(); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed closing tar"})
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Second)
		defer cancel()

		_, err = cli.CopyToContainer(ctx, id, client.CopyToContainerOptions{
			DestinationPath: destDir,
			Content:         &tarBuf,
		})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed uploading to container: %v", err)})
		}

		logAudit(userClaims.ID, userClaims.Username, "FILE_UPLOAD", "Container:"+id+" File:"+file.Filename, "Success", "Uploaded file to "+destDir)
		return c.JSON(http.StatusOK, map[string]string{"message": "File uploaded successfully"})
	}
}
