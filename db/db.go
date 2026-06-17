package db

import (
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"database/sql"
)

var GormDB *gorm.DB
var DB *sql.DB

type User struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	Username           string    `gorm:"uniqueIndex" json:"username"`
	Password           string    `json:"-"`
	Email              string    `gorm:"uniqueIndex" json:"email"`
	InviteToken        string    `json:"invite_token"`
	InviteExpiresAt    *time.Time `json:"invite_expires_at"`
	RoleTemplateID     *uint     `json:"role_template_id"`
	IsAdmin            bool      `gorm:"default:false" json:"is_admin"`
	PasswordChanged    bool      `gorm:"default:false" json:"password_changed"`
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
	IsRestrictedAccess bool      `gorm:"default:true" json:"is_restricted_access"`
	AllowedContainers  string    `gorm:"default:'.*'" json:"allowed_containers"`
	IsActive           bool      `gorm:"default:true" json:"is_active"`
	PasswordVersion    int       `gorm:"default:1" json:"password_version"`
	GoogleID           string    `json:"google_id"`
	TeamID             *uint     `json:"team_id"`
	Team               *Team     `gorm:"foreignKey:TeamID;constraint:OnDelete:SET NULL;" json:"team"`
}

type Team struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	Name              string    `gorm:"uniqueIndex;not null" json:"name"`
	Description       string    `json:"description"`
	AllowedContainers string    `gorm:"default:'.*'" json:"allowed_containers"`
	RoleTemplateID    *uint     `json:"role_template_id"`
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
	CreatedAt         time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time `gorm:"autoUpdateTime" json:"updated_at"`
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
	Timestamp time.Time `gorm:"index;autoCreateTime" json:"timestamp"`
}

type RoleTemplate struct {
	ID                 uint   `gorm:"primaryKey" json:"id"`
	Name               string `gorm:"uniqueIndex;not null" json:"name"`
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
	IsRestrictedAccess bool   `gorm:"default:true" json:"is_restricted_access"`
	AllowedContainers  string `gorm:"default:'.*'" json:"allowed_containers"`
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
	WebhookType          string `json:"webhook_type"` // slack or generic
	WebhookUrl           string `json:"webhook_url"`
	BackupEnabled        bool   `json:"backup_enabled"`
	BackupProvider       string `json:"backup_provider"` // "s3", "gcs", "azure"
	BackupCron           string `json:"backup_cron"`     // e.g. "0 0 * * *"
	BackupBucket         string `json:"backup_bucket"`
	BackupRegion         string `json:"backup_region"`
	BackupEndpoint       string `json:"backup_endpoint"`
	BackupAuth1          string `json:"backup_auth1"` // AccessKey, GCS JSON, Azure Account
	BackupAuth2          string `json:"backup_auth2"` // SecretKey, Azure Key

	ArchivalEnabled      bool   `json:"archival_enabled"`
	ArchiveMetrics       bool   `json:"archive_metrics"`
	ArchiveLogs          bool   `json:"archive_logs"`
	ArchivalProvider     string `json:"archival_provider"` // "s3", "gcs", "azure"
	ArchivalCron         string `json:"archival_cron"`     // e.g. "0 * * * *"
	ArchivalBucket       string `json:"archival_bucket"`
	ArchivalRegion       string `json:"archival_region"`
	ArchivalEndpoint     string `json:"archival_endpoint"`
	ArchivalAuth1        string `json:"archival_auth1"`
	ArchivalAuth2        string `json:"archival_auth2"`
}

type AlertRule struct {
	ID                     uint      `gorm:"primaryKey" json:"id"`
	Name                   string    `gorm:"not null;uniqueIndex" json:"name"`
	ContainerPattern       string    `gorm:"not null;default:'.*'" json:"container_pattern"`
	EventTypes             string    `gorm:"not null;default:''" json:"event_types"`
	LogPattern             string    `gorm:"not null;default:''" json:"log_pattern"`
	Enabled                bool      `gorm:"index;not null;default:true" json:"enabled"`
	CooldownSeconds        int       `gorm:"not null;default:300" json:"cooldown_seconds"`
	ChannelType            string    `gorm:"not null;default:'generic_webhook'" json:"channel_type"`
	ChannelConfig          string    `gorm:"not null;default:'{}'" json:"channel_config"`
	EnableWebhook          bool      `gorm:"not null;default:true" json:"enable_webhook"`
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
	ComposePath string    `json:"compose_path"` // Path to docker-compose.yml inside repo
	AuthToken   string    `json:"auth_token"`   // For private repos
	TargetNode  string    `json:"target_node"`  // Node ID to deploy to (empty for local)
	LastCommit  string    `json:"last_commit"`
	Status      string    `json:"status"`       // "synced", "failed", "pending"
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

type GitDeployment struct {
	ID        int       `json:"id" gorm:"primaryKey"`
	ProjectID int       `json:"project_id"`
	CommitSHA string    `json:"commit_sha"`
	Status    string    `json:"status"` // "success", "failed"
	Logs      string    `json:"logs"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

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
		{Name: "Container Crash", ContainerPattern: ".*", EventTypes: "die", MetricCpuThreshold: 0, MetricMemThreshold: 0, MetricStorageThreshold: 0, EnableWebhook: true, Enabled: true},
		{Name: "Container High CPU", ContainerPattern: ".*", MetricCpuThreshold: 85, EnableWebhook: true, Enabled: true},
		{Name: "Container High Memory", ContainerPattern: ".*", MetricMemThreshold: 85, EnableWebhook: true, Enabled: true},
		{Name: "Container Restart Loop", ContainerPattern: ".*", EventTypes: "restart", EnableWebhook: true, Enabled: true},
		{Name: "System High CPU", ContainerPattern: "system", MetricCpuThreshold: 90, EnableWebhook: true, Enabled: true},
		{Name: "System High Memory", ContainerPattern: "system", MetricMemThreshold: 90, EnableWebhook: true, Enabled: true},
		{Name: "System Low Storage", ContainerPattern: "system", MetricStorageThreshold: 90, EnableWebhook: true, Enabled: true},
		{Name: "OOM Killed", ContainerPattern: ".*", EventTypes: "oom", EnableWebhook: true, Enabled: true},
		{Name: "Deployment Failed", ContainerPattern: ".*", EventTypes: "deployment_failed", EnableWebhook: true, Enabled: true},
		{Name: "High Vulnerability Detected", ContainerPattern: ".*", EventTypes: "vulnerability_high", EnableWebhook: true, Enabled: true},
		{Name: "Image Pull BackOff", ContainerPattern: ".*", EventTypes: "image_pull_error", EnableWebhook: true, Enabled: true},
	}
	for _, r := range defaultRules {
		var existing AlertRule
		if err := GormDB.Where("name = ?", r.Name).First(&existing).Error; err != nil {
			GormDB.Create(&r)
		}
	}
}
