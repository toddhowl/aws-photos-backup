package photosbackup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/rwcarlsen/goexif/exif"
)

// Config holds configuration for the backup utility.
type Config struct {
	S3Bucket             string   `yaml:"s3_bucket"`
	PhotosLibrary        string   `yaml:"photos_library_path"`
	ZipFileName          string   `yaml:"zip_file_name"`
	LastUploadFile       string   `yaml:"last_upload_file"`
	S3KeyFormat          string   `yaml:"s3_key_format"`   // e.g. "{year}/{zip}"
	LogLevel             string   `yaml:"log_level"`       // e.g. "info", "warn", "error"
	Region               string   `yaml:"region"`          // AWS region
	TestModeLimit        int      `yaml:"test_mode_limit"` // Number of files to process in test mode
	StorageClass         string   `yaml:"storage_class"`   // S3 storage class: STANDARD, GLACIER, etc.
	AllowedExtensions    []string `yaml:"allowed_extensions"`
	MaxConcurrentUploads int      `yaml:"max_concurrent_uploads"`
}

// LoadConfig loads the YAML config file.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// GetLastUploadTime returns the last upload time from the given file.
func GetLastUploadTime(path string) time.Time {
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{} // zero time
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if err != nil {
		return time.Time{}
	}
	return t
}

// UpdateLastUploadTime writes the current time to the last upload file.
func UpdateLastUploadTime(path string) {
	now := time.Now().Format(time.RFC3339)
	_ = os.WriteFile(path, []byte(now), 0644)
}

type PhotoMeta struct {
	Path      string
	Taken     time.Time
	Camera    string
	Latitude  float64
	Longitude float64
}

// getPhotoMeta returns EXIF date, camera, and GPS info, or mod time if EXIF is missing.
func getPhotoMeta(path string) (PhotoMeta, error) {
	meta := PhotoMeta{Path: path}
	f, err := os.Open(path)
	if err != nil {
		return meta, err
	}
	defer f.Close()
	x, err := exif.Decode(f)
	if err == nil {
		dt, err := x.DateTime()
		if err == nil {
			meta.Taken = dt
		}
		if cam, err := x.Get(exif.Model); err == nil {
			meta.Camera, _ = cam.StringVal()
		}
		if lat, long, err := x.LatLong(); err == nil {
			meta.Latitude = lat
			meta.Longitude = long
		}
	}
	if meta.Taken.IsZero() {
		info, err := os.Stat(path)
		if err == nil {
			meta.Taken = info.ModTime()
		}
	}
	return meta, nil
}

// GetPhotoMetaLogged returns EXIF metadata and logs it for each photo.
func GetPhotoMetaLogged(path string) (PhotoMeta, error) {
	meta, err := getPhotoMeta(path)
	if err != nil {
		log.Printf("[ERROR] Could not extract EXIF for %s: %v", path, err)
		return meta, err
	}
	log.Printf("[EXIF] %s | Date: %s | Camera: %s | GPS: (%f, %f)",
		meta.Path, meta.Taken.Format(time.RFC3339), meta.Camera, meta.Latitude, meta.Longitude)
	return meta, nil
}

// FindNewPhotos returns a list of new photo file paths since the given time, using EXIF date if available.
// It also returns a map of excluded file extension counts.
func FindNewPhotos(root string, since time.Time, allowedExts []string) ([]string, map[string]int) {
	var files []string
	seen := make(map[string]bool)
	excluded := make(map[string]int)
	allowed := make(map[string]bool)
	for _, ext := range allowedExts {
		allowed[strings.ToLower(ext)] = true
	}
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !d.IsDir() && allowed[ext] {
			meta, err := getPhotoMeta(path)
			if err == nil && meta.Taken.After(since) {
				key := fmt.Sprintf("%s-%s", meta.Taken.Format("20060102T150405"), filepath.Base(path))
				if !seen[key] {
					files = append(files, path)
					seen[key] = true
				} else {
					log.Printf("[WARN] Duplicate photo skipped: %s", path)
				}
			}
		} else if !d.IsDir() {
			excluded[ext]++
		}
		return nil
	})
	return files, excluded
}

// GroupPhotosByYearMonth groups file paths by year and month using EXIF date if available.
func GroupPhotosByYearMonth(files []string) map[string][]string {
	result := make(map[string][]string)
	for _, file := range files {
		meta, err := getPhotoMeta(file)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%04d-%02d", meta.Taken.Year(), meta.Taken.Month())
		result[key] = append(result[key], file)
	}
	return result
}

// ZipFiles zips the given files into a zip archive.
func ZipFiles(zipName string, files []string) error {
	zipfile, err := os.Create(zipName)
	if err != nil {
		return err
	}
	defer zipfile.Close()
	zipWriter := zip.NewWriter(zipfile)
	defer zipWriter.Close()
	for _, file := range files {
		if err := addFileToZip(zipWriter, file); err != nil {
			return err
		}
	}
	return nil
}

// addFileToZip adds a file to the zip archive.
func addFileToZip(zipWriter *zip.Writer, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	w, err := zipWriter.Create(filepath.Base(filename))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, file)
	return err
}

// UploadToS3 uploads the zip file to S3 using the provided context for cancellation.
func UploadToS3(ctx context.Context, bucket, key, zipPath string, region string, storageClass string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(cfg)
	file, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer file.Close()
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	}
	if storageClass != "" {
		input.StorageClass = types.StorageClass(storageClass)
	}
	_, err = client.PutObject(ctx, input)
	return err
}

// S3Key returns the S3 key for a given year, zip name, and config.
func S3Key(cfg *Config, year, zipName string) string {
	format := cfg.S3KeyFormat
	if format == "" {
		format = "{year}/{zip}"
	}
	return strings.ReplaceAll(strings.ReplaceAll(format, "{year}", year), "{zip}", zipName)
}

// FileSHA256 computes the SHA256 checksum of a local file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// S3SHA256 downloads the S3 object and computes its SHA256 checksum.
func S3SHA256(ctx context.Context, cfg *Config, key string) (string, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return "", err
	}
	client := s3.NewFromConfig(awsCfg)
	buf := manager.NewWriteAtBuffer([]byte{})
	_, err = manager.NewDownloader(client).Download(ctx, buf, &s3.GetObjectInput{
		Bucket: &cfg.S3Bucket,
		Key:    &key,
	})
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(buf.Bytes())
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
