package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	S3Bucket       string `yaml:"s3_bucket"`
	PhotosLibrary  string `yaml:"photos_library_path"`
	ZipFileName    string `yaml:"zip_file_name"`
	LastUploadFile string `yaml:"last_upload_file"`
}

func loadConfig(path string) (*Config, error) {
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

func getLastUploadTime(path string) time.Time {
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

func updateLastUploadTime(path string) {
	now := time.Now().Format(time.RFC3339)
	_ = os.WriteFile(path, []byte(now), 0644)
}

func findNewPhotos(root string, since time.Time) []string {
	var files []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && info.ModTime().After(since) {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func groupPhotosByYearMonth(files []string) map[string][]string {
	result := make(map[string][]string)
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		t := info.ModTime()
		key := fmt.Sprintf("%04d-%02d", t.Year(), t.Month())
		result[key] = append(result[key], file)
	}
	return result
}

func zipFiles(zipName string, files []string) error {
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

func uploadToS3(bucket, key, zipPath string) error {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(cfg)
	file, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	return err
}
