package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lighthouse/db"
)

func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestDispatchCLI(t *testing.T) {
	// Setup DB for reset-password success case
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	os.Setenv("DB_PATH", dbPath)
	defer os.Unsetenv("DB_PATH")

	err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	_, err = db.DB.Exec(`CREATE TABLE IF NOT EXISTS users (username TEXT PRIMARY KEY, password TEXT, password_changed INTEGER, password_version INTEGER)`)
	if err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}
	_, err = db.DB.Exec(`INSERT INTO users (username, password) VALUES ('admin', 'oldpass')`)
	if err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}

	tests := []struct {
		args     []string
		exit     bool
		exitCode int
	}{
		{[]string{"lighthouse"}, false, 0},
		{[]string{"lighthouse", "server"}, false, 0},
		{[]string{"lighthouse", "reset-password"}, true, 1},                                  // fail
		{[]string{"lighthouse", "reset-password", "admin", "VeryLongPassword123!"}, true, 0}, // success
		{[]string{"lighthouse", "version"}, true, 0},
		{[]string{"lighthouse", "-v"}, true, 0},
		{[]string{"lighthouse", "--version"}, true, 0},
		{[]string{"lighthouse", "config"}, true, 0},
		{[]string{"lighthouse", "help"}, true, 0},
		{[]string{"lighthouse", "-h"}, true, 0},
		{[]string{"lighthouse", "--help"}, true, 0},
		{[]string{"lighthouse", "help", "server"}, true, 0},
		{[]string{"lighthouse", "help", "reset-password"}, true, 0},
		{[]string{"lighthouse", "unknown"}, true, 1},
		{[]string{"lighthouse", "-unknown"}, false, 0},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "_"), func(t *testing.T) {
			captureStderr(func() {
				captureOutput(func() {
					exit, code := dispatchCLI(tt.args)
					if exit != tt.exit || code != tt.exitCode {
						t.Errorf("expected exit=%v code=%v, got exit=%v code=%v", tt.exit, tt.exitCode, exit, code)
					}
				})
			})
		})
	}
}

func TestApplyRunMode(t *testing.T) {
	os.Unsetenv("LIGHTHOUSE_MODE")
	applyRunMode("server")
	if !serveFrontend {
		t.Errorf("expected serveFrontend to be true")
	}
	if runMode != "server" {
		t.Errorf("expected runMode to be server")
	}
	if os.Getenv("LIGHTHOUSE_MODE") != "standalone" {
		t.Errorf("expected LIGHTHOUSE_MODE to be standalone, got %v", os.Getenv("LIGHTHOUSE_MODE"))
	}

	os.Setenv("LIGHTHOUSE_MODE", "testmode")
	applyRunMode("server")
	if os.Getenv("LIGHTHOUSE_MODE") != "testmode" {
		t.Errorf("expected LIGHTHOUSE_MODE to remain testmode")
	}
}

func TestPrintVersion(t *testing.T) {
	out := captureOutput(printVersion)
	if !strings.Contains(out, Version) {
		t.Errorf("expected version %v in output, got %v", Version, out)
	}
}

func TestPrintConfig(t *testing.T) {
	os.Setenv("EXCLUDE_CONTAINERS", "test")
	os.Setenv("SECRET_KEY", "secret")
	out := captureOutput(printConfig)
	if !strings.Contains(out, "test") {
		t.Errorf("expected EXCLUDE_CONTAINERS in output")
	}
	if !strings.Contains(out, "secret_key           (set)") {
		t.Errorf("expected secret key set in output")
	}

	os.Unsetenv("EXCLUDE_CONTAINERS")
	os.Unsetenv("SECRET_KEY")
	out = captureOutput(printConfig)
	if !strings.Contains(out, "(empty — lighthouse self still hidden)") {
		t.Errorf("expected empty exclude containers in output")
	}
	if !strings.Contains(out, "(default — change in production)") {
		t.Errorf("expected default secret key in output")
	}

	// Test boolEnv fallback
	os.Setenv("ALLOW_START", "")
	os.Setenv("ALLOW_BASH", "true")
	out = captureOutput(printConfig)
	if !strings.Contains(out, "allow_start          false") {
		t.Errorf("expected ALLOW_START false in output")
	}
	if !strings.Contains(out, "allow_shell          true") {
		t.Errorf("expected ALLOW_SHELL true in output")
	}

	// Wait boolEnv allows defaults? Wait, boolEnv inside printConfig passes true/false for defaults
	// let's check code of printConfig:
	// boolEnv("ALLOW_START", false)
	// We covered the branches for boolEnv where env is empty and defaultVal is false/true
}

func TestRunModeLabel(t *testing.T) {
	os.Setenv("LIGHTHOUSE_MODE", "testlabel")
	if label := runModeLabel(); label != "testlabel" {
		t.Errorf("expected testlabel, got %v", label)
	}
	os.Setenv("LIGHTHOUSE_MODE", " ")
	runMode = "fallback"
	if label := runModeLabel(); label != "fallback" {
		t.Errorf("expected fallback, got %v", label)
	}
}

func TestEnvOrDefault(t *testing.T) {
	os.Setenv("TEST_KEY", "val")
	if envOrDefault("TEST_KEY", "fallback") != "val" {
		t.Errorf("expected val")
	}
	os.Unsetenv("TEST_KEY")
	if envOrDefault("TEST_KEY", "fallback") != "fallback" {
		t.Errorf("expected fallback")
	}
}

func TestLogRunMode(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	os.Setenv("LIGHTHOUSE_MODE", "testmode")
	logRunMode()
	if !strings.Contains(buf.String(), "testmode") {
		t.Errorf("expected testmode in output")
	}
	buf.Reset()

	os.Unsetenv("LIGHTHOUSE_MODE")
	logRunMode()
	if !strings.Contains(buf.String(), "standalone mode") {
		t.Errorf("expected standalone mode in output")
	}
}
