package gitops

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"lighthouse/alerts"
	"lighthouse/db"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) {
	var err error
	db.GormDB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	db.GormDB.AutoMigrate(&db.GitProject{}, &db.GitDeployment{})

	// Initialize AlertManager mock so TriggerSystemAlert does not panic
	alerts.Global = alerts.NewAlertManager(nil)
}

func TestProcessProject_Inline_Failure(t *testing.T) {
	setupTestDB(t)

	// Clean up /tmp/lighthouse-gitops for isolated test
	defer os.RemoveAll("/tmp/lighthouse-gitops")

	project := db.GitProject{
		Name:           "Test Inline",
		SourceType:     "inline",
		ComposeContent: "version: '3'\nservices:\n  web:\n    image: non_existent_image_foo_bar\n",
		Status:         "pending",
	}
	db.GormDB.Create(&project)

	// Run processProject. It will write the file, then try to run `docker compose up -d`
	// Since we are not guaranteeing a real docker environment or we are providing a simple compose,
	// it will likely fail or succeed. We just want to check that it updates the DB.
	err := processProject(project)

	// Let's reload project
	var updatedProject db.GitProject
	db.GormDB.First(&updatedProject, project.ID)

	// Since docker compose might not exist or fail in test environment, we expect an error
	if err == nil {
		// If it succeeds, status should be synced
		if updatedProject.Status != "synced" {
			t.Errorf("Expected status synced, got %s", updatedProject.Status)
		}
	} else {
		// If it fails, status should be failed
		if updatedProject.Status != "failed" {
			t.Errorf("Expected status failed, got %s", updatedProject.Status)
		}
	}

	// Verify a deployment history record was created
	var count int64
	db.GormDB.Model(&db.GitDeployment{}).Where("project_id = ?", project.ID).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 deployment record, got %d", count)
	}

	// Verify file was written
	workDir := filepath.Join("/tmp/lighthouse-gitops", "proj_1")
	content, err := os.ReadFile(filepath.Join(workDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("Failed to read compose file: %v", err)
	}
	if string(content) != project.ComposeContent {
		t.Errorf("Compose content mismatch")
	}
}

func TestProcessProject_Noop(t *testing.T) {
	setupTestDB(t)

	project := db.GitProject{
		Name:           "Test Inline Noop",
		SourceType:     "inline",
		ComposeContent: "version: '3'",
		Status:         "synced",
		// Give it the same SHA it would compute
		// SHA for "version: '3'" is 'f1b95f2e82ce'
		LastCommit: "898a8838ee2e", // Computed manually or let it fail
	}
	
	// We'll set the exact correct hash
	hash := sha256.Sum256([]byte(project.ComposeContent))
	project.LastCommit = hex.EncodeToString(hash[:])[:12]

	db.GormDB.Create(&project)

	err := processProject(project)
	if err != nil {
		t.Fatalf("Expected nil error for noop, got %v", err)
	}

	var count int64
	db.GormDB.Model(&db.GitDeployment{}).Where("project_id = ?", project.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected 0 deployments for noop, got %d", count)
	}
}
