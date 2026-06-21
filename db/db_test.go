package db

import (
	"os"
	"testing"

	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) {
	os.Setenv("DB_TYPE", "sqlite")
	os.Setenv("DB_DSN", ":memory:")
	
	err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize test db: %v", err)
	}
}

func TestInitDB(t *testing.T) {
	setupTestDB(t)
	
	if GormDB == nil {
		t.Fatal("Expected GormDB to be initialized")
	}
	if DB == nil {
		t.Fatal("Expected sql.DB to be initialized")
	}
}

func TestUserCRUD(t *testing.T) {
	setupTestDB(t)

	// Create
	user := User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}
	if err := GormDB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Read
	var readUser User
	if err := GormDB.First(&readUser, user.ID).Error; err != nil {
		t.Fatalf("Failed to read user: %v", err)
	}
	if readUser.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", readUser.Username)
	}

	// Update
	GormDB.Model(&readUser).Update("IsAdmin", true)
	var updatedUser User
	GormDB.First(&updatedUser, user.ID)
	if !updatedUser.IsAdmin {
		t.Error("Expected user to be admin after update")
	}

	// Delete
	if err := GormDB.Delete(&user).Error; err != nil {
		t.Fatalf("Failed to delete user: %v", err)
	}

	// Verify Delete
	if err := GormDB.First(&readUser, user.ID).Error; err != gorm.ErrRecordNotFound {
		t.Errorf("Expected record not found, got err: %v", err)
	}
}

func TestTeamCRUD(t *testing.T) {
	setupTestDB(t)

	team := Team{
		Name: "Developers",
		Description: "Dev Team",
	}
	if err := GormDB.Create(&team).Error; err != nil {
		t.Fatalf("Failed to create team: %v", err)
	}

	var readTeam Team
	if err := GormDB.First(&readTeam, team.ID).Error; err != nil {
		t.Fatalf("Failed to read team: %v", err)
	}
	if readTeam.Name != "Developers" {
		t.Errorf("Expected team name Developers, got %s", readTeam.Name)
	}

	// Assign user to team
	user := User{
		Username: "devuser",
		TeamID:   &team.ID,
	}
	if err := GormDB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create user with team: %v", err)
	}

	// Read user with preloaded team
	var userWithTeam User
	if err := GormDB.Preload("Team").First(&userWithTeam, user.ID).Error; err != nil {
		t.Fatalf("Failed to read user with team: %v", err)
	}
	if userWithTeam.Team == nil || userWithTeam.Team.Name != "Developers" {
		t.Error("Expected user to be assigned to Developers team")
	}
}

func TestAlertRuleCRUD(t *testing.T) {
	setupTestDB(t)

	rule := AlertRule{
		Name:             "High CPU Test",
		ContainerPattern: ".*",
		CooldownSeconds:  60,
		Enabled:          true,
	}
	if err := GormDB.Create(&rule).Error; err != nil {
		t.Fatalf("Failed to create alert rule: %v", err)
	}

	var readRule AlertRule
	if err := GormDB.First(&readRule, rule.ID).Error; err != nil {
		t.Fatalf("Failed to read alert rule: %v", err)
	}
	if readRule.Name != "High CPU Test" {
		t.Errorf("Expected rule name 'High CPU Test', got '%s'", readRule.Name)
	}
}

func TestGitOpsProjectCRUD(t *testing.T) {
	setupTestDB(t)

	project := GitProject{
		Name:       "Test Project",
		SourceType: "git",
		RepoURL:    "https://github.com/example/repo",
		Branch:     "main",
		Status:     "synced",
	}
	if err := GormDB.Create(&project).Error; err != nil {
		t.Fatalf("Failed to create git project: %v", err)
	}

	deployment := GitDeployment{
		ProjectID: project.ID,
		CommitSHA: "abcdef123456",
		Status:    "success",
		Logs:      "Deployed successfully",
	}
	if err := GormDB.Create(&deployment).Error; err != nil {
		t.Fatalf("Failed to create deployment: %v", err)
	}

	var readDeployment GitDeployment
	if err := GormDB.Where("project_id = ?", project.ID).First(&readDeployment).Error; err != nil {
		t.Fatalf("Failed to read deployment: %v", err)
	}
	if readDeployment.CommitSHA != "abcdef123456" {
		t.Errorf("Expected commit SHA 'abcdef123456', got '%s'", readDeployment.CommitSHA)
	}
}

func TestAuditLogEntry(t *testing.T) {
	setupTestDB(t)

	logEntry := AuditLog{
		NodeID:   "node1",
		UserID:   1,
		Username: "admin",
		Action:   "TEST_ACTION",
		Resource: "test_resource",
		Status:   "Success",
		Message:  "Test detail",
		Details:  "More detail",
	}

	if err := GormDB.Create(&logEntry).Error; err != nil {
		t.Fatalf("Failed to create audit log: %v", err)
	}

	var log AuditLog
	if err := GormDB.Last(&log).Error; err != nil {
		t.Fatalf("Failed to retrieve audit log: %v", err)
	}
	if log.Action != "TEST_ACTION" {
		t.Errorf("Expected action 'TEST_ACTION', got '%s'", log.Action)
	}
}
