package main

import (
	"fmt"
	"log"
	"os"
	"strings"
)

const Version = "1.0.0"

var (
	serveFrontend = true
	runMode       = "server"
)

func dispatchCLI(args []string) (exit bool, exitCode int) {
	if len(args) < 2 {
		applyRunMode("server")
		return false, 0
	}

	cmd := args[1]
	switch cmd {
	case "server":
		applyRunMode("server")
		return false, 0
	case "reset-password":
		if err := runResetPasswordCLI(args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "lighthouse reset-password: %v\n", err)
			return true, 1
		}
		return true, 0
	case "version", "-v", "--version":
		printVersion()
		return true, 0
	case "config":
		printConfig()
		return true, 0
	case "help", "-h", "--help":
		printCLIHelp(args[2:])
		return true, 0
	default:
		if strings.HasPrefix(cmd, "-") {
			applyRunMode("server")
			return false, 0
		}
		fmt.Fprintf(os.Stderr, "lighthouse: unknown command %q\n\n", cmd)
		printCLIHelp(nil)
		return true, 1
	}
}

func applyRunMode(mode string) {
	runMode = mode
	serveFrontend = true
	setEnvIfEmpty("LIGHTHOUSE_MODE", "standalone")
}

func setEnvIfEmpty(key, value string) {
	if strings.TrimSpace(os.Getenv(key)) == "" {
		_ = os.Setenv(key, value)
	}
}

func printVersion() {
	fmt.Printf("lighthouse %s\n", Version)
}

func printConfig() {
	boolEnv := func(key string, defaultVal bool) string {
		val := strings.TrimSpace(os.Getenv(key))
		if val == "" {
			if defaultVal {
				return "true"
			}
			return "false"
		}
		return val
	}

	fmt.Println("LightHouse configuration (non-secret):")
	fmt.Printf("  version              %s\n", Version)
	fmt.Printf("  mode                 %s\n", runModeLabel())
	fmt.Printf("  port                 %s\n", envOrDefault("PORT", "8000"))
	fmt.Printf("  db_path              %s\n", envOrDefault("DB_PATH", "lighthouse.db"))
	fmt.Printf("  docker_host          %s\n", envOrDefault("DOCKER_HOST", "unix:///var/run/docker.sock"))
	fmt.Printf("  client_access        %s\n", envOrDefault("CLIENT_ACCESS", "strict"))
	if excluded := strings.TrimSpace(os.Getenv("EXCLUDE_CONTAINERS")); excluded != "" {
		fmt.Printf("  exclude_containers     %s\n", excluded)
	} else {
		fmt.Println("  exclude_containers     (empty — lighthouse self still hidden)")
	}
	fmt.Printf("  allow_start          %s\n", boolEnv("ALLOW_START", false))
	fmt.Printf("  allow_stop           %s\n", boolEnv("ALLOW_STOP", false))
	fmt.Printf("  allow_restart        %s\n", boolEnv("ALLOW_RESTART", false))
	fmt.Printf("  allow_delete         %s\n", boolEnv("ALLOW_DELETE", false))
	allowShell := boolEnv("ALLOW_SHELL", false) == "true" || boolEnv("ALLOW_BASH", false) == "true"
	fmt.Printf("  allow_shell          %t\n", allowShell)
	if secret := strings.TrimSpace(os.Getenv("SECRET_KEY")); secret != "" {
		fmt.Println("  secret_key           (set)")
	} else {
		fmt.Println("  secret_key           (default — change in production)")
	}
}

func runModeLabel() string {
	if v := strings.TrimSpace(os.Getenv("LIGHTHOUSE_MODE")); v != "" {
		return v
	}
	return runMode
}

func envOrDefault(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func printCLIHelp(topic []string) {
	if len(topic) > 0 {
		switch topic[0] {
		case "server":
			fmt.Println(`lighthouse server — run the full LightHouse dashboard (API, WebSockets, embedded Vue UI).

Environment variables configure auth, permissions, and ports. See README.md.`)
			return
		case "reset-password":
			fmt.Println(`lighthouse reset-password <username> <new-password>

Reset a user password in the SQLite database. Invalidates existing sessions.
Requires DB_PATH to point at the on-disk database (not :memory:).`)
			return
		}
	}

	fmt.Print(`LightHouse — self-hosted Docker logs, RBAC, and monitoring.

Usage:
  lighthouse [command]

Commands:
  server          Run full dashboard with embedded web UI (default)
  reset-password  Reset a user password in SQLite
  config          Print effective non-secret configuration
  version         Print version
  help            Show this help

Examples:
  lighthouse
  lighthouse server
  lighthouse reset-password admin 'NewSecurePass1'
  lighthouse config

Install globally:
  make install
  # or: go install .

Docker:
  docker exec lighthouse lighthouse reset-password admin 'NewSecurePass1'
`)
}

func logRunMode() {
	if v := os.Getenv("LIGHTHOUSE_MODE"); v != "" {
		log.Printf("Starting LightHouse in %s mode", v)
	} else {
		log.Println("Starting LightHouse in standalone mode")
	}
}
