package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"aws-photos-backup/internal/photosbackup"
)

func main() {
	// Load configuration from config.yaml
	cfg, err := photosbackup.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Set up context for AWS SDK
	ctx := context.Background()

	// Get the last upload time from the tracking file
	lastUpload := photosbackup.GetLastUploadTime(cfg.LastUploadFile)
	// Find new photos/videos since the last upload, and get a summary of excluded file types
	newPhotos, excluded := photosbackup.FindNewPhotos(cfg.PhotosLibrary, lastUpload, cfg.AllowedExtensions)
	if len(newPhotos) == 0 {
		fmt.Println("No new photos to upload.")
	}

	// Print a summary of excluded file types (not individually logged)
	if len(excluded) > 0 {
		fmt.Println("Excluded file summary:")
		for ext, count := range excluded {
			fmt.Printf("  %s: %d\n", ext, count)
		}
	}

	// If there are no new photos/videos, exit
	if len(newPhotos) == 0 {
		return
	}

	// Collect EXIF metadata for all new photos/videos and log it
	var allMeta []photosbackup.PhotoMeta
	for _, path := range newPhotos {
		meta, err := photosbackup.GetPhotoMetaLogged(path)
		if err == nil {
			allMeta = append(allMeta, meta)
		}
	}

	// Save all metadata to a JSON file
	metaFile, err := os.Create("photo_metadata.json")
	if err != nil {
		log.Printf("[ERROR] Could not create photo_metadata.json: %v", err)
	} else {
		enc := json.NewEncoder(metaFile)
		enc.SetIndent("", "  ")
		if err := enc.Encode(allMeta); err != nil {
			log.Printf("[ERROR] Could not write photo_metadata.json: %v", err)
		}
		metaFile.Close()
		// Upload the metadata file to S3
		metaKey := "photo_metadata.json"
		if err := photosbackup.UploadToS3(ctx, cfg.S3Bucket, metaKey, "photo_metadata.json", cfg.Region, cfg.StorageClass); err != nil {
			log.Printf("[ERROR] Failed to upload photo_metadata.json: %v", err)
		} else {
			fmt.Println("[DONE] Uploaded photo_metadata.json to S3")
		}
	}

	// Group new files by year and month for zipping
	photosByYearMonth := photosbackup.GroupPhotosByYearMonth(newPhotos)

	var wg sync.WaitGroup
	var mu sync.Mutex
	failedZips, failedUploads := 0, 0

	// Set up progress bar variables
	barWidth := 40
	totalFiles := 0
	for _, files := range photosByYearMonth {
		totalFiles += len(files)
	}
	fileProgress := 0
	progressMu := sync.Mutex{}
	// Function to update the progress bar in the console
	updateBar := func(label string) {
		percent := float64(fileProgress) / float64(totalFiles)
		filled := int(percent * float64(barWidth))
		bar := strings.Repeat("\033[42m \033[0m", filled) + strings.Repeat(" ", barWidth-filled)
		fmt.Printf("\r%s [%s] %3d%% (%d/%d)", label, bar, int(percent*100), fileProgress, totalFiles)
		if fileProgress == totalFiles {
			fmt.Println()
		}
	}

	// Set up a semaphore to limit concurrency
	sem := make(chan struct{}, cfg.MaxConcurrentUploads)

	// Load upload state for resume support
	statePath := "upload_state.json"
	uploadState, _ := photosbackup.LoadUploadState(statePath)
	if uploadState == nil {
		uploadState = &photosbackup.UploadState{CompletedMonths: make(map[string]string)}
	}

	// For each year/month group, zip and upload concurrently (but limited by semaphore)
	for ym, files := range photosByYearMonth {
		// Skip months already uploaded (resume support)
		if _, done := uploadState.CompletedMonths[ym]; done {
			continue
		}
		wg.Add(1)
		go func(ym string, files []string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release
			// Add timestamp to zip file name to avoid overwriting previous zips
			timestamp := time.Now().Format("20060102T150405")
			zipName := fmt.Sprintf("%s_%s.zip", ym, timestamp)
			label := zipName // label for progress bar
			fmt.Printf("\n[START] Zipping %d files for %s\n", len(files), zipName)
			// Zip the files for this group
			if err := photosbackup.ZipFiles(zipName, files); err != nil {
				log.Printf("[ERROR] Failed to zip %s: %v", zipName, err)
				mu.Lock()
				failedZips++
				mu.Unlock()
				return
			}
			fmt.Printf("[DONE] Zipped %s\n", zipName)
			year := strings.Split(ym, "-")[0]
			s3Key := photosbackup.S3Key(cfg, year, zipName)
			// Update progress bar for each file
			for i, file := range files {
				progressMu.Lock()
				fileProgress++
				updateBar(label + fmt.Sprintf(" file %d/%d: %s", i+1, len(files), file))
				progressMu.Unlock()
			}
			fmt.Printf("[START] Uploading %s to S3 as %s\n", zipName, s3Key)
			// Retry logic for S3 upload
			var uploadErr error
			for attempt := 1; attempt <= 3; attempt++ {
				uploadErr = photosbackup.UploadToS3(ctx, cfg.S3Bucket, s3Key, zipName, cfg.Region, cfg.StorageClass)
				if uploadErr == nil {
					break
				}
				log.Printf("[WARN] Upload attempt %d for %s failed: %v", attempt, zipName, uploadErr)
				time.Sleep(time.Second * time.Duration(attempt))
			}
			if uploadErr != nil {
				log.Printf("[ERROR] Failed to upload %s after 3 attempts: %v", zipName, uploadErr)
				mu.Lock()
				failedUploads++
				mu.Unlock()
				return
			}
			// Checksum verification after upload, skip if storage class is GLACIER or DEEP_ARCHIVE
			storageClass := strings.ToUpper(cfg.StorageClass)
			if storageClass == "GLACIER" || storageClass == "DEEP_ARCHIVE" {
				log.Printf("[INFO] Skipping checksum verification for %s due to storage class %s", zipName, storageClass)
			} else {
				localSum, err := photosbackup.FileSHA256(zipName)
				if err != nil {
					log.Printf("[ERROR] Could not compute checksum for %s: %v", zipName, err)
				} else {
					remoteSum, err := photosbackup.S3SHA256(ctx, cfg, s3Key)
					if err != nil {
						log.Printf("[ERROR] Could not verify checksum for %s: %v", zipName, err)
					} else if localSum != remoteSum {
						log.Printf("[ERROR] Checksum mismatch for %s: local %s, remote %s", zipName, localSum, remoteSum)
					} else {
						fmt.Printf("[OK] Checksum verified for %s\n", zipName)
					}
				}
			}
			// Mark this month as completed in upload state
			uploadState.CompletedMonths[ym] = zipName
			photosbackup.SaveUploadState(statePath, uploadState)
			fmt.Printf("[DONE] Uploaded %s to S3\n", zipName)
			progressMu.Lock()
			updateBar(label + " uploaded!")
			progressMu.Unlock()
			os.Remove(zipName) // Remove local zip after upload
		}(ym, files)
	}
	wg.Wait()

	// Update the last upload time after all uploads are complete
	photosbackup.UpdateLastUploadTime(cfg.LastUploadFile)
	fmt.Printf("Upload complete. Failed zips: %d, failed uploads: %d\n", failedZips, failedUploads)
}
