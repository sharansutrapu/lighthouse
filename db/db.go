package db

import (
	"log"
	"os"
	"time"

	"database/sql"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var GormDB *gorm.DB
var DB *sql.DB

type User struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	Username             string     `gorm:"uniqueIndex" json:"username"`
	Password             string     `json:"-"`
	Email                string     `gorm:"uniqueIndex" json:"email"`
	InviteToken          string     `json:"invite_token"`
	InviteExpiresAt      *time.Time `json:"invite_expires_at"`
	RoleTemplateID       *uint      `json:"role_template_id"`
	IsAdmin              bool       `gorm:"default:false" json:"is_admin"`
	PasswordChanged      bool       `gorm:"default:false" json:"password_changed"`
	CanStart             bool       `gorm:"default:false" json:"can_start"`
	CanStop              bool       `gorm:"default:false" json:"can_stop"`
	CanRestart           bool       `gorm:"default:false" json:"can_restart"`
	CanDelete            bool       `gorm:"default:false" json:"can_delete"`
	CanShell             bool       `gorm:"default:false" json:"can_shell"`
	CanViewSystemHealth  bool       `gorm:"default:false" json:"can_view_system_health"`
	CanRunScans          bool       `gorm:"default:false" json:"can_run_scans"`
	CanCreateDeployments bool       `gorm:"default:false" json:"can_create_deployments"`
	CanEditDeployments   bool       `gorm:"default:false" json:"can_edit_deployments"`
	CanDeleteDeployments bool       `gorm:"default:false" json:"can_delete_deployments"`
	IsRestrictedAccess   bool       `gorm:"default:true" json:"is_restricted_access"`
	AllowedContainers    string     `gorm:"default:'.*'" json:"allowed_containers"`
	IsActive             bool       `gorm:"default:true" json:"is_active"`
	PasswordVersion      int        `gorm:"default:1" json:"password_version"`
	GoogleID             string     `json:"google_id"`
	TeamID               *uint      `json:"team_id"`
	Team                 *Team      `gorm:"foreignKey:TeamID;constraint:OnDelete:SET NULL;" json:"team"`
}

type Team struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	Name                 string    `gorm:"uniqueIndex;not null" json:"name"`
	Description          string    `json:"description"`
	AllowedContainers    string    `gorm:"default:'.*'" json:"allowed_containers"`
	RoleTemplateID       *uint     `json:"role_template_id"`
	CanStart             bool      `gorm:"default:false" json:"can_start"`
	CanStop              bool      `gorm:"default:false" json:"can_stop"`
	CanRestart           bool      `gorm:"default:false" json:"can_restart"`
	CanDelete            bool      `gorm:"default:false" json:"can_delete"`
	CanShell             bool      `gorm:"default:false" json:"can_shell"`
	CanViewSystemHealth  bool      `gorm:"default:false" json:"can_view_system_health"`
	CanRunScans          bool      `gorm:"default:false" json:"can_run_scans"`
	CanCreateDeployments bool      `gorm:"default:false" json:"can_create_deployments"`
	CanEditDeployments   bool      `gorm:"default:false" json:"can_edit_deployments"`
	CanDeleteDeployments bool      `gorm:"default:false" json:"can_delete_deployments"`
	AlertsEmailAddress   string    `gorm:"default:''" json:"alerts_email_address"`
	SlackWebhookUrl      string    `gorm:"default:''" json:"slack_webhook_url"`
	MSTeamsWebhookUrl    string    `gorm:"default:''" json:"msteams_webhook_url"`
	GChatWebhookUrl      string    `gorm:"default:''" json:"gchat_webhook_url"`
	GenericWebhookUrl    string    `gorm:"default:''" json:"generic_webhook_url"`
	CreatedAt            time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

type Stat struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	NodeID         string    `gorm:"index" json:"node_id"`
	ContainerID    string    `gorm:"index:idx_stats_container_time" json:"container_id"`
	CPU            float64   `json:"cpu"`
	Memory         int64     `json:"memory"`
	NetRxBytes     int64     `gorm:"default:0" json:"net_rx_bytes"`
	NetTxBytes     int64     `gorm:"default:0" json:"net_tx_bytes"`
	DiskReadBytes  int64     `gorm:"default:0" json:"disk_read_bytes"`
	DiskWriteBytes int64     `gorm:"default:0" json:"disk_write_bytes"`
	Timestamp      time.Time `gorm:"index:idx_stats_container_time;autoCreateTime" json:"timestamp"`
}

type SystemStat struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	NodeID         string    `gorm:"index" json:"node_id"`
	CPU            float64   `json:"cpu"`
	Memory         int64     `json:"memory"`
	NetRxBytes     int64     `gorm:"default:0" json:"net_rx_bytes"`
	NetTxBytes     int64     `gorm:"default:0" json:"net_tx_bytes"`
	DiskReadBytes  int64     `gorm:"default:0" json:"disk_read_bytes"`
	DiskWriteBytes int64     `gorm:"default:0" json:"disk_write_bytes"`
	Timestamp      time.Time `gorm:"index;autoCreateTime" json:"timestamp"`
}

type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	NodeID    string    `gorm:"index" json:"node_id"`
	UserID    uint      `json:"user_id"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Details   string    `json:"details"`
	Timestamp time.Time `gorm:"index;autoCreateTime" json:"timestamp"`
}

type RoleTemplate struct {
	ID                   uint   `gorm:"primaryKey" json:"id"`
	Name                 string `gorm:"uniqueIndex;not null" json:"name"`
	CanStart             bool   `gorm:"default:false" json:"can_start"`
	CanStop              bool   `gorm:"default:false" json:"can_stop"`
	CanRestart           bool   `gorm:"default:false" json:"can_restart"`
	CanDelete            bool   `gorm:"default:false" json:"can_delete"`
	CanShell             bool   `gorm:"default:false" json:"can_shell"`
	CanViewSystemHealth  bool   `gorm:"default:false" json:"can_view_system_health"`
	CanRunScans          bool   `gorm:"default:false" json:"can_run_scans"`
	CanCreateDeployments bool   `gorm:"default:false" json:"can_create_deployments"`
	CanEditDeployments   bool   `gorm:"default:false" json:"can_edit_deployments"`
	CanDeleteDeployments bool   `gorm:"default:false" json:"can_delete_deployments"`
	IsRestrictedAccess   bool   `gorm:"default:true" json:"is_restricted_access"`
	AllowedContainers    string `gorm:"default:'.*'" json:"allowed_containers"`
}

type Setting struct {
	ID                   uint   `gorm:"primaryKey" json:"id"`
	MetricsRetentionDays int    `gorm:"default:30" json:"metrics_retention_days"`
	SmtpHost             string `gorm:"default:''" json:"smtp_host"`
	SmtpPort             int    `gorm:"default:587" json:"smtp_port"`
	SmtpUser             string `gorm:"default:''" json:"smtp_user"`
	SmtpPass             string `gorm:"default:''" json:"smtp_pass"`
	GoogleClientID       string `gorm:"default:''" json:"google_client_id"`
	GoogleClientSecret   string `gorm:"default:''" json:"google_client_secret"`
	SlackWebhookUrl      string `gorm:"default:''" json:"slack_webhook_url"`
	MSTeamsWebhookUrl    string `gorm:"default:''" json:"msteams_webhook_url"`
	GChatWebhookUrl      string `gorm:"default:''" json:"gchat_webhook_url"`
	GenericWebhookUrl    string `gorm:"default:''" json:"generic_webhook_url"`
	AlertsEmailAddress   string `gorm:"default:''" json:"alerts_email_address"`
	BackupEnabled        bool   `json:"backup_enabled"`
	BackupProvider       string `json:"backup_provider"` // "s3", "gcs", "azure"
	BackupCron           string `json:"backup_cron"`     // e.g. "0 0 * * *"
	BackupBucket         string `json:"backup_bucket"`
	BackupRegion         string `json:"backup_region"`
	BackupEndpoint       string `json:"backup_endpoint"`
	BackupAuth1          string `json:"backup_auth1"` // AccessKey, GCS JSON, Azure Account
	BackupAuth2          string `json:"backup_auth2"` // SecretKey, Azure Key

	ArchivalEnabled  bool   `json:"archival_enabled"`
	ArchiveMetrics   bool   `json:"archive_metrics"`
	ArchiveLogs      bool   `json:"archive_logs"`
	ArchivalProvider string `json:"archival_provider"` // "s3", "gcs", "azure"
	ArchivalCron     string `json:"archival_cron"`     // e.g. "0 * * * *"
	ArchivalBucket   string `json:"archival_bucket"`
	ArchivalRegion   string `json:"archival_region"`
	ArchivalEndpoint string `json:"archival_endpoint"`
	ArchivalAuth1    string `json:"archival_auth1"`
	ArchivalAuth2    string `json:"archival_auth2"`

	AutoScanEnabled  bool   `json:"auto_scan_enabled"`
}

type AlertRule struct {
	ID                     uint      `gorm:"primaryKey" json:"id"`
	Name                   string    `gorm:"not null;uniqueIndex" json:"name"`
	ContainerPattern       string    `gorm:"not null;default:'.*'" json:"container_pattern"`
	EventTypes             string    `gorm:"not null;default:''" json:"event_types"`
	LogPattern             string    `gorm:"not null;default:''" json:"log_pattern"`
	Enabled                bool      `gorm:"index;not null;default:true" json:"enabled"`
	CooldownSeconds        int       `gorm:"not null;default:300" json:"cooldown_seconds"`
	EnableSlack            bool      `gorm:"not null;default:false" json:"enable_slack"`
	EnableMSTeams          bool      `gorm:"not null;default:false" json:"enable_msteams"`
	EnableGChat            bool      `gorm:"not null;default:false" json:"enable_gchat"`
	EnableGenericWebhook   bool      `gorm:"not null;default:true" json:"enable_generic_webhook"`
	EnableEmail            bool      `gorm:"not null;default:false" json:"enable_email"`
	EmailAddress           string    `gorm:"not null;default:''" json:"email_address"`
	MetricCpuThreshold     float64   `gorm:"default:0" json:"metric_cpu_threshold"`
	MetricMemThreshold     int64     `gorm:"default:0" json:"metric_mem_threshold"`
	MetricStorageThreshold int64     `gorm:"default:0" json:"metric_storage_threshold"`
	CreatedAt              time.Time `gorm:"autoCreateTime" json:"created_at"`
}

type AlertHistory struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	NodeID          string     `gorm:"index" json:"node_id"`
	RuleID          *uint      `gorm:"index" json:"rule_id"`
	RuleName        string     `json:"rule_name"`
	ContainerName   string     `json:"container_name"`
	AlertType       string     `json:"alert_type"`
	Details         string     `json:"details"`
	DeliveryStatus  string     `gorm:"default:''" json:"delivery_status"`
	DeliveryChannel string     `gorm:"default:''" json:"delivery_channel"`
	Timestamp       time.Time  `gorm:"index;autoCreateTime" json:"timestamp"`
	AlertRule       *AlertRule `gorm:"foreignKey:RuleID;constraint:OnDelete:SET NULL;" json:"-"`
}

type Node struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex" json:"name"`
	Address   string    `json:"address"` // e.g., http://192.168.1.10:8080
	Token     string    `json:"token"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

type ImageScanResult struct {
	ID        int       `json:"id" gorm:"primaryKey"`
	Image     string    `json:"image" gorm:"index"`
	Result    string    `json:"result"` // JSON string of Trivy output
	CreatedAt time.Time `json:"created_at"`
}

type GitProject struct {
	ID             int       `json:"id" gorm:"primaryKey"`
	Name           string    `json:"name"`
	SourceType     string    `json:"source_type"`     // "git" or "inline"
	ComposeContent string    `json:"compose_content"` // inline YAML
	RepoURL        string    `json:"repo_url"`
	Branch         string    `json:"branch"`
	ComposePath    string    `json:"compose_path"` // Path to docker-compose.yml inside repo
	AuthToken      string    `json:"auth_token"`   // For private repos
	TargetNode     string    `json:"target_node"`  // Node ID to deploy to (empty for local)
	LastCommit     string    `json:"last_commit"`
	Status         string    `json:"status"` // "synced", "failed", "pending"
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

type GitDeployment struct {
	ID        int       `json:"id" gorm:"primaryKey"`
	ProjectID int       `json:"project_id"`
	CommitSHA string    `json:"commit_sha"`
	Status    string    `json:"status"` // "success", "failed"
	Logs      string    `json:"logs"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

type ApiToken struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	Name      string    `json:"name"`
	Token     string    `gorm:"uniqueIndex" json:"token"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
}

// OnAuditLogged can be set to be called whenever an audit log is created.
var OnAuditLogged func(action, resource, status, details string)

func InitDB(dataSourceName string) error {
	var err error

	dbType := os.Getenv("DB_TYPE")
	if dbType == "" {
		dbType = "sqlite"
	}
	dbDSN := os.Getenv("DB_DSN")
	if dbDSN == "" {
		if dbType == "sqlite" {
			dbDSN = dataSourceName
		}
	}

	config := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
	}

	if dbType == "postgres" {
		GormDB, err = gorm.Open(postgres.Open(dbDSN), config)
	} else {
		GormDB, err = gorm.Open(sqlite.Open(dbDSN), config)
	}

	if err != nil {
		return err
	}

	DB, err = GormDB.DB()
	if err != nil {
		return err
	}

	// Optimize DB Connection Pooling
	// SQLite is a single-file DB — only 1 writer can hold the lock at a time.
	// Using more than 1 connection causes "database is locked" errors that silently
	// drop metrics, settings and audit log writes. PostgreSQL keeps a full pool.
	if dbType == "postgres" {
		DB.SetMaxOpenConns(100)
		DB.SetMaxIdleConns(20)
		DB.SetConnMaxLifetime(time.Hour)
	} else {
		// Single connection for SQLite to eliminate lock contention
		DB.SetMaxOpenConns(1)
		DB.SetMaxIdleConns(1)
		DB.SetConnMaxLifetime(0) // keep connection alive; SQLite has no server-side timeout

		// Enable WAL mode for crash-safe writes — data survives hard stops and OOM kills.
		// busy_timeout prevents "database is locked" on the rare write collision.
		// synchronous=NORMAL gives a good balance of durability vs. performance.
		_, _ = DB.Exec("PRAGMA journal_mode=WAL")
		_, _ = DB.Exec("PRAGMA busy_timeout=5000")
		_, _ = DB.Exec("PRAGMA synchronous=NORMAL")
		// 8 MB page cache — sufficient for a metrics DB with 30s write cadence.
		// The previous 64 MB was pinning 64 MB permanently in the Go heap.
		_, _ = DB.Exec("PRAGMA cache_size=-8000")
	}

	err = GormDB.AutoMigrate(
		&User{},
		&Team{},
		&Stat{},
		&SystemStat{},
		&AuditLog{},
		&RoleTemplate{},
		&Setting{},
		&AlertRule{},
		&AlertHistory{},
		&Node{},
		&ImageScanResult{},
		&GitProject{},
		&GitDeployment{},
		&ApiToken{},
	)
	if err != nil {
		log.Printf("AutoMigrate failed: %v", err)
		return err
	}

	seedDefaults()
	return nil
}

func seedDefaults() {
	// Settings
	var count int64
	GormDB.Model(&Setting{}).Where("id = ?", 1).Count(&count)
	if count == 0 {
		GormDB.Create(&Setting{ID: 1, MetricsRetentionDays: 30})
	}

	// Role Templates
	defaultRoles := []RoleTemplate{
		{ID: 1, Name: "Full Admin", CanStart: true, CanStop: true, CanRestart: true, CanDelete: true, CanShell: true, IsRestrictedAccess: false, AllowedContainers: ".*"},
		{ID: 2, Name: "Read-Only Observer", CanStart: false, CanStop: false, CanRestart: false, CanDelete: false, CanShell: false, IsRestrictedAccess: false, AllowedContainers: ".*"},
	}
	for _, r := range defaultRoles {
		var existing RoleTemplate
		if err := GormDB.Where("name = ?", r.Name).First(&existing).Error; err != nil {
			GormDB.Create(&r)
		}
	}

	// Default Alert Rules
	defaultRules := []AlertRule{
		{Name: "Container Crash", ContainerPattern: ".*", EventTypes: "die", MetricCpuThreshold: 0, MetricMemThreshold: 0, MetricStorageThreshold: 0, EnableGenericWebhook: true, Enabled: false},
		{Name: "Container High CPU", ContainerPattern: ".*", MetricCpuThreshold: 85, EnableGenericWebhook: true, Enabled: false},
		{Name: "Container High Memory", ContainerPattern: ".*", MetricMemThreshold: 85, EnableGenericWebhook: true, Enabled: false},
		// NOTE: Docker does not emit a 'restart' event. Container restarts are
		// detected via rapid 'die' events. We listen to 'die' with a short cooldown.
		{Name: "Container Restart Loop", ContainerPattern: ".*", EventTypes: "die", CooldownSeconds: 60, EnableGenericWebhook: true, Enabled: false},
		{Name: "System High CPU", ContainerPattern: "system", MetricCpuThreshold: 90, EnableGenericWebhook: true, Enabled: false},
		{Name: "System High Memory", ContainerPattern: "system", MetricMemThreshold: 90, EnableGenericWebhook: true, Enabled: false},
		{Name: "System Low Storage", ContainerPattern: "system", MetricStorageThreshold: 90, EnableGenericWebhook: true, Enabled: false},
		{Name: "OOM Killed", ContainerPattern: ".*", EventTypes: "oom", EnableGenericWebhook: true, Enabled: false},
		{Name: "Deployment Failed", ContainerPattern: ".*", EventTypes: "deployment_failed", EnableGenericWebhook: true, Enabled: false},
		{Name: "High Vulnerability Detected", ContainerPattern: ".*", EventTypes: "vulnerability_found", EnableGenericWebhook: true, Enabled: false},
		{Name: "Image Pull BackOff", ContainerPattern: ".*", EventTypes: "image_pull_error", EnableGenericWebhook: true, Enabled: false},
		{Name: "Container Stopped", ContainerPattern: ".*", EventTypes: "stop", EnableGenericWebhook: true, Enabled: false},
		{Name: "Container Killed", ContainerPattern: ".*", EventTypes: "kill", EnableGenericWebhook: true, Enabled: false},
		{Name: "System Audit Event", ContainerPattern: "system", EventTypes: "audit", EnableGenericWebhook: true, Enabled: false},
		{Name: "Authentication Failed", ContainerPattern: "system", EventTypes: "auth_failed", EnableGenericWebhook: true, Enabled: false},
		{Name: "GitOps Sync Failed", ContainerPattern: "system", EventTypes: "gitops_failed", EnableGenericWebhook: true, Enabled: false},
		{Name: "Backup Failed", ContainerPattern: "system", EventTypes: "backup_failed", EnableGenericWebhook: true, Enabled: false},
	}
	for _, r := range defaultRules {
		var existing AlertRule
		if err := GormDB.Where("name = ?", r.Name).First(&existing).Error; err != nil {
			GormDB.Create(&r)
		}
	}

	// Hotfix: Ensure "High Vulnerability Detected" rule has correct event type if it was created with the bugged value previously
	GormDB.Model(&AlertRule{}).Where("name = ? AND event_types = ?", "High Vulnerability Detected", "vulnerability_high").Update("event_types", "vulnerability_found")

	// Hotfix: "Container Restart Loop" was created with event_types='restart' which doesn't exist in Docker.
	// Fix to 'die' with a 60s cooldown so rapid container deaths are detected.
	GormDB.Model(&AlertRule{}).Where("name = ? AND event_types = ?", "Container Restart Loop", "restart").
		Updates(map[string]interface{}{"event_types": "die", "cooldown_seconds": 60})
}

