package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"lighthouse/alerts"
	"lighthouse/db"

	"github.com/robfig/cron/v3"
)

var scheduler *cron.Cron
var currentEntryID cron.EntryID

// InitScheduler starts the backup background process
func InitScheduler() {
	if scheduler == nil {
		scheduler = cron.New()
		scheduler.Start()
	}
	ReloadSchedule()
}

// ReloadSchedule fetches the current backup config and schedules the cron job
func ReloadSchedule() {
	var settings db.Setting
	if err := db.GormDB.First(&settings, 1).Error; err != nil {
		return
	}

	if currentEntryID != 0 {
		scheduler.Remove(currentEntryID)
		currentEntryID = 0
	}

	if settings.BackupEnabled && settings.BackupCron != "" {
		id, err := scheduler.AddFunc(settings.BackupCron, func() {
			log.Println("[Backup] Starting scheduled backup...")
			err := RunBackup(settings)
			if err != nil {
				log.Printf("[Backup] Failed: %v", err)
				alerts.Global.TriggerSystemAlert("backup_failed", fmt.Sprintf("Scheduled backup to %s failed: %v", settings.BackupProvider, err))
			} else {
				log.Println("[Backup] Successfully completed scheduled backup")
				alerts.Global.TriggerSystemAlert("backup_success", fmt.Sprintf("Scheduled backup to %s completed successfully.", settings.BackupProvider))
			}
		})
		if err == nil {
			currentEntryID = id
			log.Printf("[Backup] Scheduled cron job: %s", settings.BackupCron)
		} else {
			log.Printf("[Backup] Invalid cron expression: %v", err)
		}
	}
}

// RunBackup zips the database and uploads to the configured cloud provider
func RunBackup(s db.Setting) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var provider StorageProvider
	var err error

	switch s.BackupProvider {
	case "s3":
		provider, err = NewS3Provider(s.BackupEndpoint, s.BackupAuth1, s.BackupAuth2, s.BackupRegion)
	case "gcs":
		provider, err = NewGCSProvider(ctx, s.BackupAuth1)
	case "azure":
		provider, err = NewAzureProvider(s.BackupAuth1, s.BackupAuth2)
	default:
		return fmt.Errorf("unknown backup provider: %s", s.BackupProvider)
	}
	if err != nil {
		return fmt.Errorf("failed to initialize provider %s: %v", s.BackupProvider, err)
	}

	// Create gzip
	dbPath := "lighthouse.db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return fmt.Errorf("database file %s not found", dbPath)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	archiveName := fmt.Sprintf("lighthouse_backup_%s.tar.gz", timestamp)
	archivePath := filepath.Join(os.TempDir(), archiveName)

	if err := compressFile(dbPath, archivePath); err != nil {
		return fmt.Errorf("failed to compress database: %v", err)
	}
	defer os.Remove(archivePath)

	// Upload
	if err := provider.Upload(ctx, s.BackupBucket, archiveName, archivePath); err != nil {
		return fmt.Errorf("upload failed: %v", err)
	}

	return nil
}

func compressFile(src, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, info.Name())
	if err != nil {
		return err
	}
	header.Name = filepath.Base(src)

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if _, err := io.Copy(tw, file); err != nil {
		return err
	}
	return nil
}
