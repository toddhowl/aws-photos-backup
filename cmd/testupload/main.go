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
	cfg, err := photosbackup.LoadConfig("../config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	ctx := context.Background()
	lastUpload := photosbackup.GetLastUploadTime(cfg.LastUploadFile)
	newFiles, excluded := photosbackup.FindNewPhotos(cfg.PhotosLibrary, lastUpload, cfg.AllowedExtensions)
	if len(newFiles) == 0 {
		fmt.Println("No new photos or videos to upload.")
	}

	// Print summary of excluded file types
	if len(excluded) > 0 {
		fmt.Println("Excluded file summary:")
		for ext, count := range excluded {
			fmt.Printf("  %s: %d\n", ext, count)
		}
	}

	if len(newFiles) == 0 {
		return
	}

	limit := cfg.TestModeLimit
	if limit > 0 && len(newFiles) > limit {
		newFiles = newFiles[:limit]
	}

	// Collect metadata for all new files and log EXIF info (for photos)
	var allMeta []photosbackup.PhotoMeta
	for _, path := range newFiles {
		meta, err := photosbackup.GetPhotoMetaLogged(path)
		if err == nil {
			allMeta = append(allMeta, meta)
		}
	}
	// Save metadata to JSON file
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
		// Upload photo_metadata.json to S3
		metaKey := "photo_metadata.json"
		if err := photosbackup.UploadToS3(ctx, cfg.S3Bucket, metaKey, "photo_metadata.json", cfg.Region, cfg.StorageClass); err != nil {
			log.Printf("[ERROR] Failed to upload photo_metadata.json: %v", err)
		} else {
			fmt.Println("[DONE] Uploaded photo_metadata.json to S3")
		}
	}

	filesByYearMonth := photosbackup.GroupPhotosByYearMonth(newFiles)

	var wg sync.WaitGroup
	var mu sync.Mutex
	failedZips, failedUploads := 0, 0

	// Advanced progress bar setup
	barWidth := 40
	totalFiles := 0
	for _, files := range filesByYearMonth {
		totalFiles += len(files)
	}
	fileProgress := 0
	progressMu := sync.Mutex{}
	updateBar := func(label string) {
		percent := float64(fileProgress) / float64(totalFiles)
		filled := int(percent * float64(barWidth))
		bar := strings.Repeat("\033[42m \033[0m", filled) + strings.Repeat(" ", barWidth-filled)
		fmt.Printf("\r%s [%s] %3d%% (%d/%d)", label, bar, int(percent*100), fileProgress, totalFiles)
		if fileProgress == totalFiles {
			fmt.Println()
		}
	}

	for ym, files := range filesByYearMonth {
		wg.Add(1)
		go func(ym string, files []string) {
			defer wg.Done()
			// Add timestamp to zip file name to avoid overwriting previous test zips
			timestamp := time.Now().Format("20060102T150405")
			zipName := fmt.Sprintf("test-%s_%s.zip", ym, timestamp)
			label := zipName // label for progress bar
			fmt.Printf("\n[START] Zipping %d files for %s\n", len(files), zipName)
			// Zip the files for this group
			if err := photosbackup.ZipFiles(zipName, files); err != nil {
				log.Printf("[ERROR] Failed to zip %s: %v", zipName, err)
				mu.Lock()
				failedZips++
				mu.Unlock()
			} else {
				fmt.Printf("[DONE] Zipped %s\n", zipName)
				year := strings.Split(ym, "-")[0]
				s3Key := fmt.Sprintf("test/%s/%s", year, zipName)
				// Update progress bar for each file
				for i, file := range files {
					progressMu.Lock()
					fileProgress++
					updateBar(label + fmt.Sprintf(" file %d/%d: %s", i+1, len(files), file))
					progressMu.Unlock()
				}
				fmt.Printf("[START] Uploading %s to S3 as %s\n", zipName, s3Key)
				// Upload the zip file to S3
				if err := photosbackup.UploadToS3(ctx, cfg.S3Bucket, s3Key, zipName, cfg.Region, cfg.StorageClass); err != nil {
					log.Printf("[ERROR] Failed to upload %s: %v", zipName, err)
					mu.Lock()
					failedUploads++
					mu.Unlock()
				} else {
					fmt.Printf("[DONE] Uploaded %s to S3\n", zipName)
					progressMu.Lock()
					updateBar(label + " uploaded!")
					progressMu.Unlock()
					os.Remove(zipName)
				}
			}
		}(ym, files)
	}
	wg.Wait()
	fmt.Printf("Test upload complete. Failed zips: %d, failed uploads: %d\n", failedZips, failedUploads)
}
