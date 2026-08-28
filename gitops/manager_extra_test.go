package gitops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"lighthouse/db"
)

// TestProcessProject_Git_Success drives processProject through a full,
// successful git-based deployment: clone succeeds, rev-parse succeeds, and
// `docker compose up` succeeds. None of the existing tests exercised this
// full happy path (they only ever test individual failure branches), which
// left the clone-success/rev-parse-success lines at 0% coverage.
func TestProcessProject_Git_Success(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = origExec }()

	for _, v := range []string{"FAIL_CLONE", "FAIL_FETCH", "FAIL_RESET", "FAIL_REVPARSE", "FAIL_DOCKER"} {
		os.Unsetenv(v)
	}

	p := db.GitProject{
		ID:          9001,
		Name:        "git-success",
		RepoURL:     "https://example.com/repo.git",
		Branch:      "main",
		ComposePath: "docker-compose.yml",
		Status:      "pending",
	}

	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	os.RemoveAll(workDir) // ensure no .git present -> exercises the clone path
	defer os.RemoveAll(workDir)

	if err := processProject(p); err != nil {
		t.Fatalf("processProject (full git success path) = %v, want nil", err)
	}

	var deployment db.GitDeployment
	if err := db.GormDB.Where("project_id = ?", p.ID).First(&deployment).Error; err != nil {
		t.Fatalf("expected a deployment record to be created: %v", err)
	}
	if deployment.Status != "success" {
		t.Errorf("deployment.Status = %q, want success", deployment.Status)
	}
}

// TestProcessProject_Git_ExistingRepo_FetchResetSuccess pre-creates a .git
// directory so processProject takes the "existing repo" branch (git fetch +
// git reset --hard) instead of git clone — that branch was entirely
// untested (0% coverage) since every existing test operates on a fresh
// workDir.
func TestProcessProject_Git_ExistingRepo_FetchResetSuccess(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = origExec }()

	for _, v := range []string{"FAIL_CLONE", "FAIL_FETCH", "FAIL_RESET", "FAIL_REVPARSE", "FAIL_DOCKER"} {
		os.Unsetenv(v)
	}

	p := db.GitProject{
		ID:          9002,
		Name:        "git-existing",
		RepoURL:     "https://example.com/repo2.git",
		Branch:      "main",
		ComposePath: "docker-compose.yml",
		Status:      "pending",
	}

	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	if err := os.MkdirAll(filepath.Join(workDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to pre-create .git dir: %v", err)
	}
	defer os.RemoveAll(workDir)

	if err := processProject(p); err != nil {
		t.Fatalf("processProject (existing repo, fetch+reset) = %v, want nil", err)
	}
}

// TestProcessProject_AuthTokenAskPass covers the GIT_ASKPASS credential
// injection branch, which is skipped whenever AuthToken is empty in every
// other test.
func TestProcessProject_AuthTokenAskPass(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = origExec }()

	for _, v := range []string{"FAIL_CLONE", "FAIL_FETCH", "FAIL_RESET", "FAIL_REVPARSE", "FAIL_DOCKER"} {
		os.Unsetenv(v)
	}

	p := db.GitProject{
		ID:          9003,
		Name:        "git-with-token",
		RepoURL:     "https://example.com/private-repo.git",
		Branch:      "main",
		ComposePath: "docker-compose.yml",
		AuthToken:   "super-secret-token",
		Status:      "pending",
	}

	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	os.RemoveAll(workDir)
	defer os.RemoveAll(workDir)

	if err := processProject(p); err != nil {
		t.Fatalf("processProject (with auth token) = %v, want nil", err)
	}
}

// TestProcessProject_ExistingRepo_FailureBranches isolates the fetch/reset/
// rev-parse failure paths (each in its own subtest with its own workDir and
// carefully scoped env vars) since global FAIL_* env var pollution between
// tests in this package can otherwise make one test's failure branch never
// actually execute.
func TestProcessProject_ExistingRepo_FailureBranches(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	execCommand = mockExecCommand
	defer func() { execCommand = origExec }()

	tests := []struct {
		name   string
		id     int
		failOn string
	}{
		{name: "fetch fails", id: 9101, failOn: "FAIL_FETCH"},
		{name: "reset fails", id: 9102, failOn: "FAIL_RESET"},
		{name: "rev-parse fails", id: 9103, failOn: "FAIL_REVPARSE"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, v := range []string{"FAIL_CLONE", "FAIL_FETCH", "FAIL_RESET", "FAIL_REVPARSE", "FAIL_DOCKER"} {
				os.Unsetenv(v)
			}
			os.Setenv(tc.failOn, "1")
			defer os.Unsetenv(tc.failOn)

			p := db.GitProject{
				ID:          tc.id,
				Name:        tc.name,
				RepoURL:     "https://example.com/repo.git",
				Branch:      "main",
				ComposePath: "docker-compose.yml",
				Status:      "pending",
			}

			workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
			if err := os.MkdirAll(filepath.Join(workDir, ".git"), 0755); err != nil {
				t.Fatalf("failed to pre-create .git dir: %v", err)
			}
			defer os.RemoveAll(workDir)

			if err := processProject(p); err == nil {
				t.Fatalf("processProject with %s=1 = nil, want an error", tc.failOn)
			}
		})
	}
}

