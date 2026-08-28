// Package scanner wraps the Trivy CLI to provide on-demand and scheduled
// container image vulnerability scanning, storing results for display in the
// dashboard and feeding critical findings into the alerting engine.
package scanner

import (
	"context"
	"encoding/json"
	"log"

	"github.com/moby/moby/client"
	"github.com/robfig/cron/v3"
	"lighthouse/db"
)

var scheduler *cron.Cron
var currentEntryID cron.EntryID

// AlertCallback allows higher levels to inject alerting logic to avoid import cycles
var AlertCallback func(imageName string, resultBytes []byte)

// init starts the package-wide cron scheduler immediately at process start;
// ReloadSchedule then adds/removes the actual scan job based on settings.
func init() {
	scheduler = cron.New()
	scheduler.Start()
}

// ReloadSchedule fetches the current settings and schedules the cron job
func ReloadSchedule(cli *client.Client) {
	var settings db.Setting
	if err := db.GormDB.First(&settings, 1).Error; err != nil {
		log.Println("[Scanner] Failed to fetch settings for scheduled scans:", err)
		return
	}

	// Remove existing schedule if any
	if currentEntryID != 0 {
		scheduler.Remove(currentEntryID)
		currentEntryID = 0
	}

	if settings.ScheduledScanEnabled && settings.ScheduledScanCron != "" {
		id, err := scheduler.AddFunc(settings.ScheduledScanCron, func() {
			RunScheduledScans(cli)
		})
		if err == nil {
			currentEntryID = id
			log.Printf("[Scanner] Scheduled cron job: %s", settings.ScheduledScanCron)
		} else {
			log.Printf("[Scanner] Invalid cron expression: %v", err)
		}
	} else {
		log.Println("[Scanner] Scheduled scans are disabled or no cron provided.")
	}
}

// RunScheduledScans runs a scan on all currently running containers
func RunScheduledScans(cli *client.Client) {
	log.Println("[Scanner] Starting scheduled vulnerability scan sweep...")
	containers, err := cli.ContainerList(context.Background(), client.ContainerListOptions{All: false})
	if err != nil {
		log.Printf("[Scanner] Failed to list containers for scheduled scan: %v", err)
		return
	}

	for _, c := range containers.Items {
		imageName := c.Image
		if imageName == "" {
			continue
		}

		_, _ = ExecuteAndSaveScan(context.Background(), cli, imageName)
	}
	log.Println("[Scanner] Scheduled vulnerability scan sweep complete.")
}

// ExecuteAndSaveScan scans the image and saves the result to the DB
func ExecuteAndSaveScan(ctx context.Context, cli *client.Client, imageName string) (map[string]interface{}, error) {
	res, err := ScanImageFunc(ctx, cli, imageName)
	if err != nil {
		return nil, err
	}

	b, _ := json.Marshal(res)

	err = db.GormDB.Create(&db.ImageScanResult{
		Image:  imageName,
		Result: string(b),
	}).Error

	if err != nil {
		log.Printf("[Scanner] Failed to save scan result for %s: %v", imageName, err)
	}

	if AlertCallback != nil {
		AlertCallback(imageName, b)
	}

	return res, nil
}
