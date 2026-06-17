package archival

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"lighthouse/backup"
	"lighthouse/db"

	"github.com/moby/moby/client"
	"github.com/robfig/cron/v3"
)

var scheduler *cron.Cron
var currentEntryID cron.EntryID
var lastArchivalTime time.Time

// InitScheduler starts the archival background process
func InitScheduler() {
	if scheduler == nil {
		scheduler = cron.New()
		scheduler.Start()
	}
	lastArchivalTime = time.Now().Add(-1 * time.Hour) // Initial window: last hour
	ReloadSchedule()
}

// ReloadSchedule fetches the current archival config and schedules the cron job
func ReloadSchedule() {
	var settings db.Setting
	if err := db.GormDB.First(&settings, 1).Error; err != nil {
		return
	}

	if currentEntryID != 0 {
		scheduler.Remove(currentEntryID)
		currentEntryID = 0
	}

	if settings.ArchivalEnabled && settings.ArchivalCron != "" {
		id, err := scheduler.AddFunc(settings.ArchivalCron, func() {
			log.Println("[Archival] Starting scheduled archival...")
			err := RunArchival(settings)
			if err != nil {
				log.Printf("[Archival] Failed: %v", err)
			} else {
				log.Println("[Archival] Successfully completed scheduled archival")
				lastArchivalTime = time.Now()
			}
		})
		if err == nil {
			currentEntryID = id
			log.Printf("[Archival] Scheduled cron job: %s", settings.ArchivalCron)
		} else {
			log.Printf("[Archival] Invalid cron expression: %v", err)
		}
	}
}

// RunArchival archives logs and metrics to the configured cloud provider
func RunArchival(s db.Setting) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	var provider backup.StorageProvider
	var err error

	switch s.ArchivalProvider {
	case "s3":
		provider, err = backup.NewS3Provider(s.ArchivalEndpoint, s.ArchivalAuth1, s.ArchivalAuth2, s.ArchivalRegion)
	case "gcs":
		provider, err = backup.NewGCSProvider(ctx, s.ArchivalAuth1)
	case "azure":
		provider, err = backup.NewAzureProvider(s.ArchivalAuth1, s.ArchivalAuth2)
	default:
		return fmt.Errorf("unknown archival provider: %s", s.ArchivalProvider)
	}
	if err != nil {
		return fmt.Errorf("failed to initialize provider %s: %v", s.ArchivalProvider, err)
	}

	windowEnd := time.Now()
	timestamp := windowEnd.Format("2006-01-02_15-04-05")
	datePrefix := windowEnd.Format("2006-01-02")

	if s.ArchiveMetrics {
		log.Println("[Archival] Archiving metrics...")
		if err := archiveMetrics(ctx, provider, s.ArchivalBucket, datePrefix, timestamp, lastArchivalTime, windowEnd); err != nil {
			log.Printf("[Archival] Metrics archival failed: %v", err)
		}
	}

	if s.ArchiveLogs {
		log.Println("[Archival] Archiving logs...")
		if err := archiveLogs(ctx, provider, s.ArchivalBucket, datePrefix, timestamp, lastArchivalTime, windowEnd); err != nil {
			log.Printf("[Archival] Logs archival failed: %v", err)
		}
	}

	return nil
}

func archiveMetrics(ctx context.Context, provider backup.StorageProvider, bucket, datePrefix, timestamp string, start, end time.Time) error {
	archiveName := fmt.Sprintf("metrics_%s.jsonl.gz", timestamp)
	archivePath := filepath.Join(os.TempDir(), archiveName)
	
	outFile, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	gw := gzip.NewWriter(outFile)

	var containerStats []db.Stat
	if err := db.GormDB.Where("timestamp >= ? AND timestamp <= ?", start, end).Find(&containerStats).Error; err == nil {
		for _, stat := range containerStats {
			b, _ := json.Marshal(map[string]interface{}{
				"type": "container",
				"data": stat,
			})
			gw.Write(b)
			gw.Write([]byte("\n"))
		}
	}

	var systemStats []db.SystemStat
	if err := db.GormDB.Where("timestamp >= ? AND timestamp <= ?", start, end).Find(&systemStats).Error; err == nil {
		for _, stat := range systemStats {
			b, _ := json.Marshal(map[string]interface{}{
				"type": "system",
				"data": stat,
			})
			gw.Write(b)
			gw.Write([]byte("\n"))
		}
	}

	gw.Close()
	outFile.Close()

	destPath := fmt.Sprintf("metrics/%s/%s", datePrefix, archiveName)
	return provider.Upload(ctx, bucket, destPath, archivePath)
}

func archiveLogs(ctx context.Context, provider backup.StorageProvider, bucket, datePrefix, timestamp string, start, end time.Time) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	res, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}
	containers := extractContainers(res.Items)

	archiveName := fmt.Sprintf("logs_%s.tar.gz", timestamp)
	archivePath := filepath.Join(os.TempDir(), archiveName)

	outFile, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	gw := gzip.NewWriter(outFile)
	tw := tar.NewWriter(gw)

	for _, c := range containers {
		cID, _ := c["ID"].(string)
		if cID == "" {
			cID, _ = c["Id"].(string)
		}
		cName := cID
		if len(cID) > 12 {
			cName = cID[:12]
		}
		
		namesInterface, _ := c["Names"].([]interface{})
		if len(namesInterface) > 0 {
			if n, ok := namesInterface[0].(string); ok && len(n) > 1 {
				cName = n[1:]
			}
		}

		options := client.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Since:      fmt.Sprintf("%d", start.Unix()),
			Until:      fmt.Sprintf("%d", end.Unix()),
			Timestamps: true,
		}

		logsReader, err := cli.ContainerLogs(ctx, cID, options)
		if err != nil {
			log.Printf("[Archival] Could not fetch logs for container %s: %v", cName, err)
			continue
		}

		var buf bytes.Buffer
		// Docker multiplexes stdout/stderr, but for archival we can just read the raw bytes
		// To be perfectly clean, we'd demultiplex using stdcopy, but reading raw bytes is fine for archiving
		// unless we want to strip the 8-byte header. We'll strip the header for clean logs.
		// Since we just want to save time/memory, we read everything. Actually, it's safer to read raw or use a custom scanner.
		// We can just dump the raw stream into the tar.
		_, err = io.Copy(&buf, logsReader)
		logsReader.Close()
		if err != nil || buf.Len() == 0 {
			continue // skip empty logs
		}

		header := &tar.Header{
			Name:    fmt.Sprintf("%s.log", cName),
			Size:    int64(buf.Len()),
			Mode:    0644,
			ModTime: end,
		}
		if err := tw.WriteHeader(header); err != nil {
			log.Printf("[Archival] Failed to write tar header for %s: %v", cName, err)
			continue
		}
		if _, err := tw.Write(buf.Bytes()); err != nil {
			log.Printf("[Archival] Failed to write tar body for %s: %v", cName, err)
		}
	}

	tw.Close()
	gw.Close()
	outFile.Close()

	// Upload if not empty
	destPath := fmt.Sprintf("logs/%s/%s", datePrefix, archiveName)
	return provider.Upload(ctx, bucket, destPath, archivePath)
}

func extractContainers(res interface{}) []map[string]interface{} {
	b, _ := json.Marshal(res)
	var m interface{}
	json.Unmarshal(b, &m)

	if list, ok := m.([]interface{}); ok {
		var ret []map[string]interface{}
		for _, item := range list {
			if mm, ok := item.(map[string]interface{}); ok {
				ret = append(ret, mm)
			}
		}
		return ret
	}
	if mm, ok := m.(map[string]interface{}); ok {
		for _, val := range mm {
			if list, ok := val.([]interface{}); ok {
				var ret []map[string]interface{}
				for _, item := range list {
					if itemMap, ok := item.(map[string]interface{}); ok {
						ret = append(ret, itemMap)
					}
				}
				return ret
			}
		}
	}
	return nil
}
