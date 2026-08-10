package gitops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lighthouse/alerts"
	"lighthouse/db"
)

// Helper script to mock exec.Command
func helperProcess(args ...string) {
	if len(os.Args) > 2 && os.Args[1] == "-test.run=TestHelperProcess" {
		cmdStr := os.Args[3]
		// Mock depending on command
		if cmdStr == "git" {
			if len(os.Args) > 4 && os.Args[4] == "clone" {
				if os.Getenv("FAIL_CLONE") == "1" {
					fmt.Fprintf(os.Stderr, "clone failed")
					os.Exit(1)
				}
				// Mock git clone: create a fake .git directory so it succeeds
				os.MkdirAll(".git", 0755)
				os.Exit(0)
			}
			if len(os.Args) > 4 && os.Args[4] == "fetch" {
				if os.Getenv("FAIL_FETCH") == "1" {
					fmt.Fprintf(os.Stderr, "fetch failed")
					os.Exit(1)
				}
				os.Exit(0)
			}
			if len(os.Args) > 4 && os.Args[4] == "reset" {
				if os.Getenv("FAIL_RESET") == "1" {
					fmt.Fprintf(os.Stderr, "reset failed")
					os.Exit(1)
				}
				os.Exit(0)
			}
			if len(os.Args) > 4 && os.Args[4] == "rev-parse" {
				if os.Getenv("FAIL_REVPARSE") == "1" {
					os.Exit(1)
				}
				fmt.Print("mock_commit_sha")
				os.Exit(0)
			}
		} else if cmdStr == "docker" {
			if os.Getenv("FAIL_DOCKER") == "1" {
				fmt.Fprintf(os.Stderr, "docker failed")
				os.Exit(1)
			}
			os.Exit(0)
		}
		os.Exit(0)
	}
}

func TestHelperProcess(t *testing.T) {
	helperProcess()
}

func mockExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	return cmd
}

func setupTestDB(t *testing.T) {
	err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	db.GormDB.AutoMigrate(&db.GitProject{}, &db.GitDeployment{})
	alerts.Global = alerts.NewAlertManager(nil)
}

func TestStartManager(t *testing.T) {
	setupTestDB(t)
	// Replace interval
	origInterval := syncInterval
	defer func() { syncInterval = origInterval }()
	syncInterval = 1 * time.Millisecond

	StartManager()
	time.Sleep(10 * time.Millisecond) // Let goroutine tick
}

func TestProcessProjects_DBError(t *testing.T) {
	setupTestDB(t)
	db.GormDB.Migrator().DropTable(&db.GitProject{})
	// processProjects should log error and return
	processProjects()
}

func TestProcessProject_TargetNode(t *testing.T) {
	setupTestDB(t)
	err := processProject(db.GitProject{TargetNode: "spoke-1"})
	if err == nil || err.Error() != "spoke deployment not fully implemented for gitops yet" {
		t.Errorf("expected spoke error, got %v", err)
	}
}

func TestProcessProject_MkdirError(t *testing.T) {
	setupTestDB(t)
	t.Setenv("TMPDIR", t.TempDir()) // override temp dir
	// To cause MkdirAll to fail, create a file at the target directory
	p := db.GitProject{ID: 100}
	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	os.MkdirAll(filepath.Dir(workDir), 0755)
	os.WriteFile(workDir, []byte("file"), 0644) // file where dir should be

	err := processProject(p)
	if err == nil {
		t.Errorf("expected mkdir error, got nil")
	}
}

func TestProcessProject_Inline_ComposeWriteError(t *testing.T) {
	setupTestDB(t)
	t.Setenv("TMPDIR", t.TempDir())
	p := db.GitProject{ID: 200, SourceType: "inline"}
	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	os.MkdirAll(workDir, 0755)

	// Make a directory called docker-compose.yml so WriteFile fails
	os.Mkdir(filepath.Join(workDir, "docker-compose.yml"), 0755)

	err := processProject(p)
	if err == nil {
		t.Errorf("expected write error, got nil")
	}
}

func TestProcessProject_Git_CloneFail(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = mockExecCommand

	t.Setenv("FAIL_CLONE", "1")
	t.Setenv("TMPDIR", t.TempDir())

	p := db.GitProject{
		ID:         300,
		SourceType: "git",
		RepoURL:    "https://foo.com/bar.git",
		Branch:     "main",
		AuthToken:  "secret-token",
	}
	db.GormDB.Create(&p)

	err := processProject(p)
	if err == nil || err.Error() == "" {
		t.Errorf("expected clone error, got %v", err)
	}
}

func TestProcessProject_Git_FetchFail(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = mockExecCommand

	t.Setenv("FAIL_FETCH", "1")
	t.Setenv("TMPDIR", t.TempDir())

	p := db.GitProject{ID: 301, SourceType: "git"}
	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	os.MkdirAll(filepath.Join(workDir, ".git"), 0755) // simulate already cloned

	err := processProject(p)
	if err == nil {
		t.Errorf("expected fetch error")
	}
}

func TestProcessProject_Git_ResetFail(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = mockExecCommand

	t.Setenv("FAIL_RESET", "1")
	t.Setenv("TMPDIR", t.TempDir())

	p := db.GitProject{ID: 302, SourceType: "git"}
	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	os.MkdirAll(filepath.Join(workDir, ".git"), 0755)

	err := processProject(p)
	if err == nil {
		t.Errorf("expected reset error")
	}
}

func TestProcessProject_Git_RevParseFail(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = mockExecCommand

	t.Setenv("FAIL_REVPARSE", "1")
	t.Setenv("TMPDIR", t.TempDir())

	p := db.GitProject{ID: 303, SourceType: "git"}
	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	os.MkdirAll(filepath.Join(workDir, ".git"), 0755)

	err := processProject(p)
	if err == nil {
		t.Errorf("expected rev-parse error")
	}
}

func TestProcessProject_DockerDeployFail(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = mockExecCommand

	t.Setenv("FAIL_DOCKER", "1")
	t.Setenv("TMPDIR", t.TempDir())

	p := db.GitProject{ID: 304, SourceType: "git", Name: "FailDeploy", ComposePath: "../bad/docker-compose.yml"}
	db.GormDB.Create(&p)

	err := processProject(p)
	if err == nil {
		t.Errorf("expected docker deploy error")
	}
}

func TestProcessProject_Success_Inline(t *testing.T) {
	setupTestDB(t)
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = mockExecCommand
	t.Setenv("TMPDIR", t.TempDir())

	p := db.GitProject{
		ID:             400,
		Name:           "Success Inline",
		SourceType:     "inline",
		ComposeContent: "version: '3'",
	}
	db.GormDB.Create(&p)

	err := processProject(p)
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
}

func TestProcessProject_Noop(t *testing.T) {
	setupTestDB(t)
	t.Setenv("TMPDIR", t.TempDir())

	content := "version: '3'"
	hash := sha256.Sum256([]byte(content))
	commitSHA := hex.EncodeToString(hash[:])[:12]

	p := db.GitProject{
		Name:           "Test Inline Noop",
		SourceType:     "inline",
		ComposeContent: content,
		Status:         "synced",
		LastCommit:     commitSHA,
	}
	db.GormDB.Create(&p)

	err := processProject(p)
	if err != nil {
		t.Fatalf("Expected nil error for noop, got %v", err)
	}
}

func TestProcessProjects_Loop(t *testing.T) {
	setupTestDB(t)
	t.Setenv("TMPDIR", t.TempDir())
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = mockExecCommand

	// One success, one fail to cover the loop
	p1 := db.GitProject{Name: "P1", SourceType: "inline", ComposeContent: "c1"}
	p2 := db.GitProject{Name: "P2", SourceType: "git", TargetNode: "nodeX"} // force error
	db.GormDB.Create(&p1)
	db.GormDB.Create(&p2)

	processProjects()
}
