# AWS Photos & Videos Backup Utility

This utility scans your macOS Photos library for new photos and videos, organizes them by year and month, zips each month's new files, and uploads the archives to an AWS S3 bucket. It is written in Go and designed to run weekly, uploading only new files since the last backup.

## Features
- Recursively scans your Photos library for new photo and video files since the last upload
- **Processes image and video file types:** configurable in `allowed_extensions` in `config.yaml` (default includes `.jpg`, `.jpeg`, `.png`, `.heic`, `.tiff`, `.crw`, `.dng`, `.mov`, `.mp4`, `.avi`, `.m4v`, `.hevc`, `.3gp`, `.3g2`)
- Skips non-media files and reports a summary of excluded file types after each run
- Groups new files by year and month
- Zips each month's new files into a separate archive with a unique timestamp (e.g., `2025-06_20250621T153000.zip`)
- Uploads each zip file to your specified S3 bucket in a folder for the year (e.g., `2025/2025-06_20250621T153000.zip`)
- **Configurable S3 storage class:** Uploads can be stored in STANDARD, GLACIER, DEEP_ARCHIVE, etc., as set in `config.yaml`
- Remembers the last upload time to avoid duplicate uploads
- Extracts EXIF metadata (date, camera model, GPS, etc.) for each photo (where available)
- Handles duplicate files (same EXIF date/name) gracefully
- Reads all configuration from a YAML file
- **Test mode**: Uploads only a configurable number of files for testing (see `test_mode_limit` in config)
- **Concurrent zipping and uploading** for faster performance
- Configurable S3 key structure and log level

## How It Works
- On each run, the tool finds all new media files (by EXIF date or mod time) since the last successful upload.
- New files are grouped by year and month, zipped, and uploaded to S3.
- Each zip file is named with the year, month, and a timestamp to avoid overwriting previous backups.
- The last upload time is updated only after all uploads complete successfully.
- The test process uses the same logic, but uploads to a test folder and can be limited by `test_mode_limit`.

## Project Structure
- `cmd/photos_backup.go`: Main Go program for full backup (now concurrent, timestamped zips)
- `cmd/testupload/main.go`: Test script to upload only a sample of files (configurable, timestamped zips)
- `internal/photosbackup/photosbackup.go`: Shared library for backup logic
- `config.yaml`: Configuration file for S3 bucket, library path, etc.
- `go.mod`, `go.sum`: Go module and dependency files
- `last_upload.txt`: Stores the timestamp of the last successful upload (auto-created)

## Prerequisites
- Go 1.21 or later
- AWS account and S3 bucket
- AWS credentials configured (via `~/.aws/credentials`, environment variables, or IAM role)

## Configuration
Edit `config.yaml` in the project root:

```yaml
s3_bucket: howell-mac-photos-backup
photos_library_path: /Users/todd/Pictures/Photos Library.photoslibrary/originals
zip_file_name: photos_backup.zip  # (not used in grouped mode, but required)
last_upload_file: last_upload.txt
s3_key_format: "{year}/{zip}"
log_level: "info"
region: us-east-1
test_mode_limit: 10  # Number of files to process in test mode
storage_class: STANDARD  # Options: STANDARD, GLACIER, DEEP_ARCHIVE, etc.
allowed_extensions:
  - .jpg
  - .jpeg
  - .png
  - .heic
  - .tiff
  - .crw
  - .dng
  - .mov
  - .mp4
  - .avi
  - .m4v
  - .hevc
  - .3gp
  - .3g2
```
- `s3_bucket`: Your S3 bucket name
- `photos_library_path`: Path to your Photos library originals
- `last_upload_file`: File to track last upload time
- `s3_key_format`: S3 key structure (default `{year}/{zip}`)
- `log_level`: Logging level (future use)
- `test_mode_limit`: Number of files to process in test mode (for test script)
- `storage_class`: S3 storage class for uploaded zips. Use `STANDARD` for regular S3, `GLACIER` or `DEEP_ARCHIVE` for archival storage.
- `allowed_extensions`: List of file extensions to include in backup. You can add or remove types as needed.

## Usage
### Install dependencies
```sh
go mod tidy
```

### Run the full backup (concurrent)
This will scan for all new photos and videos, group by year/month, zip, and upload to S3 concurrently:
```sh
go run ./cmd/photos_backup.go
```

### Run a test upload (configurable number of files)
This will only upload a sample of new photos/videos for testing, using a `test/` prefix in S3. The number of files is set by `test_mode_limit` in your config:
```sh
go run ./cmd/testupload/main.go
```

### Using VS Code Tasks
You can also run these from the VS Code Command Palette:
- **Run Full Backup**: runs the main backup script
- **Run Test Upload (sample files)**: runs the test script

Open the Command Palette (`Cmd+Shift+P`), search for "Run Task", and select the task you want.

## Automating Weekly Backups
You can automate the utility to run weekly using `cron` or macOS `launchd`.

### Using cron
1. Open your crontab:
   ```sh
   crontab -e
   ```
2. Add a line to run the backup every Sunday at 2am:
   ```sh
   0 2 * * 0 cd /Users/todd/Documents/Git/aws-photos-backup && /usr/local/go/bin/go run ./cmd/photos_backup.go
   ```
   - Adjust the path to `go` if needed (`which go` to find it).

### Using launchd (macOS)
- For more reliability on macOS, consider using a `launchd` plist. See [Apple's launchd documentation](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html).

## Notes
- The utility creates a `last_upload.txt` file in the project directory to track uploads.
- AWS credentials must be available in your environment.
- The utility only uploads new or modified files since the last run.
- Zip files are deleted locally after upload.
- S3 key structure and log level are configurable in `config.yaml`.
- **Non-media files are skipped and a warning is logged.**
- **EXIF metadata (date, camera, GPS) is extracted and can be stored or logged for photos.**
- **Duplicate files (same EXIF date/name) are detected and handled gracefully.**

## Troubleshooting

- **No files are uploaded:**
  - Check that your `allowed_extensions` in `config.yaml` matches your actual file types.
  - Ensure the `photos_library_path` is correct and accessible.
  - Make sure `last_upload.txt` is not set to a future date.
- **AWS upload errors:**
  - Verify your AWS credentials and permissions.
  - Check your S3 bucket name and region in `config.yaml`.
  - Ensure your network connection is stable.
- **Permission denied errors:**
  - Run the tool with sufficient permissions to read your Photos library and write to the project directory.
- **EXIF errors or missing dates:**
  - Some files may lack EXIF data; the tool will fall back to file modification time.
- **Slow uploads or high resource usage:**
  - For large libraries, consider increasing available system resources or limiting concurrency (see below).

## Concurrency

By default, the tool launches one goroutine per year/month group for zipping and uploading. If you have many months with new files, this could result in high concurrency. To control this, you can add a `max_concurrent_uploads` setting to your `config.yaml` and update the code to use a semaphore or worker pool.

---
## License
MIT


