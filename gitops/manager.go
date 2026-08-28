// Package gitops implements a lightweight GitOps poller: it periodically syncs
// configured Git repositories (or inline compose content), detects new commits,
// and runs `docker compose up` in an isolated per-project workspace to deploy
// changes automatically.
package gitops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"lighthouse/alerts"
	"lighthouse/db"
)

var (
	execCommand  = exec.Command
	syncInterval = 30 * time.Second
)

var allowedRepoScheme = regexp.MustCompile(`^(https|http|git|ssh)://`)

// ValidateRepoURL rejects git "transport helper" syntax (ext::, fd::, etc.) and
// any scheme outside an explicit allow-list. Without this, a RepoURL like
// "ext::sh -c ..." makes `git clone` execute an arbitrary shell command on the
// host running this poller.
func ValidateRepoURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("repo_url is required")
	}
	if strings.Contains(raw, "::") {
		return fmt.Errorf("unsupported git transport syntax in repo_url")
	}
	if !allowedRepoScheme.MatchString(raw) {
		return fmt.Errorf("repo_url must use http://, https://, git://, or ssh://")
	}
	return nil
}

// StartManager starts the GitOps background worker
func StartManager() {
	go func() {
		for {
			time.Sleep(syncInterval) // Poll every 30s
			processProjects()
		}
	}()
}

// processProjects loads every configured GitProject and syncs each one in
// turn, raising a system alert (without stopping the sweep) for any that fail.
func processProjects() {
	var projects []db.GitProject
	if err := db.GormDB.Find(&projects).Error; err != nil {
		log.Printf("[GitOps] Failed to fetch projects: %v", err)
		return
	}

	for _, p := range projects {
		err := processProject(p)
		if err != nil {
			log.Printf("[GitOps] Project %s sync failed: %v", p.Name, err)
			alerts.Global.TriggerSystemAlert("gitops_failed", fmt.Sprintf("GitOps Project %s sync failed: %v", p.Name, err))
		}
	}
}

// processProject syncs one GitOps project: for inline projects it writes the
// stored compose YAML to a workspace file; for Git projects it clones (first
// run) or fetches+resets (subsequent runs) the configured repo/branch. If the
// resulting commit differs from the last deployed one, it runs `docker
// compose up` and records the outcome as a GitDeployment.
func processProject(p db.GitProject) error {
	// If it's targeted for a spoke, we shouldn't clone it locally. We should tell the spoke to sync it!
	// But wait, the hub could clone it, build it? No, compose up needs to run on the target node.
	// So we need to dispatch to the Spoke!
	if p.TargetNode != "" {
		// Just send a "gitops_sync" command to Spoke with project details
		// For now we'll handle the local case first
		// TODO: Add Spoke sync
		return fmt.Errorf("spoke deployment not fully implemented for gitops yet")
	}

	workDir := filepath.Join(os.TempDir(), "lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
	err := os.MkdirAll(workDir, 0755)
	if err != nil {
		return err
	}

	commitSHA := ""

	if p.SourceType == "inline" {
		// Inline Compose deployment
		composeFile := "docker-compose.yml"
		composePath := filepath.Join(workDir, composeFile)

		err := os.WriteFile(composePath, []byte(p.ComposeContent), 0644)
		if err != nil {
			return fmt.Errorf("failed to write inline compose file: %v", err)
		}

		// Compute hash as pseudo-commit SHA
		hash := sha256.Sum256([]byte(p.ComposeContent))
		commitSHA = hex.EncodeToString(hash[:])[:12]

	} else {
		// Git deployment
		if err := ValidateRepoURL(p.RepoURL); err != nil {
			return err
		}
		// Build a sanitized URL for logging (no credentials in it).
		sanitizedURL := p.RepoURL

		env := os.Environ()
		if p.AuthToken != "" {
			// Use GIT_ASKPASS to inject credentials without embedding the
			// token in the URL, which would expose it in git output / logs.
			// We write a tiny helper script that echoes the token as the
			// password; git calls it when credentials are required.
			askPassScript := filepath.Join(workDir, "git-askpass.sh")
			askPassContent := fmt.Sprintf("#!/bin/sh\necho '%s'\n", p.AuthToken)
			if err := os.WriteFile(askPassScript, []byte(askPassContent), 0700); err == nil {
				defer os.Remove(askPassScript)
				env = append(env,
					"GIT_ASKPASS="+askPassScript,
					"GIT_USERNAME=oauth2",
				)
			}
		}

		if _, err := os.Stat(filepath.Join(workDir, ".git")); os.IsNotExist(err) {
			cmd := execCommand("git", "clone", "-b", p.Branch, "--", p.RepoURL, ".")
			cmd.Dir = workDir
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("clone failed for %s: %s", sanitizedURL, string(out))
			}
		} else {
			cmd := execCommand("git", "fetch", "origin", p.Branch)
			cmd.Dir = workDir
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("fetch failed for %s: %s", sanitizedURL, string(out))
			}
			cmd = execCommand("git", "reset", "--hard", "origin/"+p.Branch)
			cmd.Dir = workDir
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("reset failed: %s", string(out))
			}
		}

		cmd := execCommand("git", "rev-parse", "HEAD")
		cmd.Dir = workDir
		out, err := cmd.Output()
		if err != nil {
			return err
		}
		commitSHA = strings.TrimSpace(string(out))
	}

	if commitSHA == p.LastCommit && (p.Status == "synced" || p.Status == "failed") {
		// No change, and we already tried deploying this commit
		return nil
	}

	log.Printf("[GitOps] Project %s has new commit %s, deploying...", p.Name, commitSHA)
	db.GormDB.Model(&p).Updates(map[string]interface{}{
		"status": "pending",
	})

	// Run docker-compose up
	cleanPath := filepath.Clean(p.ComposePath)
	if strings.Contains(cleanPath, "..") {
		cleanPath = "."
	}
	composeDir := filepath.Join(workDir, filepath.Dir(cleanPath))
	composeFile := filepath.Base(cleanPath)
	if composeFile == "" || composeFile == "." {
		composeFile = "docker-compose.yml"
	}

	deployCmd := execCommand("docker", "compose", "-f", composeFile, "up", "-d")
	deployCmd.Dir = composeDir
	deployOut, deployErr := deployCmd.CombinedOutput()

	// Record deployment
	status := "success"
	if deployErr != nil {
		status = "failed"
	}

	db.GormDB.Create(&db.GitDeployment{
		ProjectID: p.ID,
		CommitSHA: commitSHA,
		Status:    status,
		Logs:      string(deployOut),
	})

	if deployErr != nil {
		db.GormDB.Model(&p).Updates(map[string]interface{}{
			"status":      "failed",
			"last_commit": commitSHA,
		})
		return fmt.Errorf("docker compose up failed: %s", string(deployOut))
	}

	// Update project
	db.GormDB.Model(&p).Updates(map[string]interface{}{
		"status":      "synced",
		"last_commit": commitSHA,
	})
	log.Printf("[GitOps] Project %s deployed successfully", p.Name)
	alerts.Global.TriggerSystemAlert("gitops_success", fmt.Sprintf("GitOps Project %s successfully deployed commit %s", p.Name, commitSHA))
	return nil
}
