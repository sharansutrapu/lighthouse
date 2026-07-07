package gitops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"lighthouse/alerts"
	"lighthouse/db"
)

// StartManager starts the GitOps background worker
func StartManager() {
	go func() {
		for {
			time.Sleep(30 * time.Second) // Poll every 30s
			processProjects()
		}
	}()
}

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

	workDir := filepath.Join("/tmp/lighthouse-gitops", fmt.Sprintf("proj_%d", p.ID))
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
			cmd := exec.Command("git", "clone", "-b", p.Branch, "--", p.RepoURL, ".")
			cmd.Dir = workDir
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("clone failed for %s: %s", sanitizedURL, string(out))
			}
		} else {
			cmd := exec.Command("git", "fetch", "origin", p.Branch)
			cmd.Dir = workDir
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("fetch failed for %s: %s", sanitizedURL, string(out))
			}
			cmd = exec.Command("git", "reset", "--hard", "origin/"+p.Branch)
			cmd.Dir = workDir
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("reset failed: %s", string(out))
			}
		}

		cmd := exec.Command("git", "rev-parse", "HEAD")
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

	deployCmd := exec.Command("docker", "compose", "-f", composeFile, "up", "-d")
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
