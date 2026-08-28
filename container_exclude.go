// This file controls which containers are hidden from users: the LightHouse
// platform container itself is always hidden (from non-admins), plus any
// operator-configured EXCLUDE_CONTAINERS names. It also redacts
// secret-looking environment variable values before they reach the client.
package main

import (
	"os"
	"strings"
)

// excludedContainerNames is the parsed EXCLUDE_CONTAINERS allow-list,
// populated once at boot by initExcludedContainers.
var excludedContainerNames []string

// initExcludedContainers parses the EXCLUDE_CONTAINERS env var (a
// comma-separated list of container names) into excludedContainerNames.
func initExcludedContainers() {
	raw := strings.TrimSpace(os.Getenv("EXCLUDE_CONTAINERS"))
	if raw == "" {
		excludedContainerNames = nil
		return
	}

	parts := strings.Split(raw, ",")
	excludedContainerNames = make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		excludedContainerNames = append(excludedContainerNames, name)
	}
}

// isLightHouseSelfContainer reports whether a container is the LightHouse
// dashboard's own container (by name "lighthouse" or an image containing
// "lighthouse"), which non-admins should never see or be able to stop.
func isLightHouseSelfContainer(name, image string) bool {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if name != "" && strings.EqualFold(name, "lighthouse") {
		return true
	}
	return strings.Contains(strings.ToLower(image), "lighthouse")
}

// isExcludedContainer reports whether a container should be hidden: either
// it's the LightHouse platform container, or its name matches an entry in
// EXCLUDE_CONTAINERS.
func isExcludedContainer(name, image string) bool {
	if isLightHouseSelfContainer(name, image) {
		return true
	}

	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	for _, excluded := range excludedContainerNames {
		if excluded == "" {
			continue
		}
		if strings.EqualFold(name, excluded) {
			return true
		}
	}
	return false
}

// sanitizeContainerEnv redacts the values of environment variables whose
// name looks secret-bearing (contains "pass", "secret", "key", "token",
// "auth", "pwd", or "db_") before container inspect data is sent to the client.
func sanitizeContainerEnv(env []string) []string {
	sanitized := make([]string, len(env))
	for i, item := range env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			sanitized[i] = item
			continue
		}
		k := strings.ToLower(parts[0])
		if strings.Contains(k, "pass") ||
			strings.Contains(k, "secret") ||
			strings.Contains(k, "key") ||
			strings.Contains(k, "token") ||
			strings.Contains(k, "auth") ||
			strings.Contains(k, "pwd") ||
			strings.Contains(k, "db_") {
			sanitized[i] = parts[0] + "=••••••••••••"
			continue
		}
		sanitized[i] = item
	}
	return sanitized
}

// containerNameImageFromInspect normalizes the leading-slash container name
// Docker returns from inspect calls.
func containerNameImageFromInspect(name string, configImage string) (string, string) {
	return strings.TrimPrefix(strings.TrimSpace(name), "/"), configImage
}

// inspectContainerExcluded is the check handlers run after ContainerInspect:
// admins bypass exclusion entirely, everyone else is blocked from the
// LightHouse platform container and any EXCLUDE_CONTAINERS entry.
func inspectContainerExcluded(isAdmin bool, name, configImage string) bool {
	if isAdmin {
		return false
	}
	containerName, containerImage := containerNameImageFromInspect(name, configImage)
	if isLightHouseSelfContainer(containerName, containerImage) {
		return false
	}
	return isExcludedContainer(containerName, containerImage)
}
