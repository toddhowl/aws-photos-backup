# AWS Photos & Videos Backup Utility

This Go utility scans your macOS Photos library for new photos and videos, organizes them by year and month, zips each month's new files, and uploads the archives to an AWS S3 bucket. It is designed for regular (e.g., weekly) use, uploading only new files since the last backup. It supports full and test modes, resume, concurrency, and checksum verification.

---

## Features

- **Scans for new photos and videos** since the last upload, using EXIF date or file modification time
- **Configurable file types**: set in `allowed_extensions` in `config.yaml` (default includes most common photo/video formats)
- **Skips non-media files** and reports a summary of excluded file types after each run
- **Groups new files by year and month**
- **Zips each month's new files** into a separate archive with a unique timestamp (e.g., `2025-06_20250701T153000.zip`)
- **Uploads each zip file to S3** in a year-based folder (e.g., `2025/2025-06_20250701T153000.zip`)
- **Configurable S3 storage class**: STANDARD, GLACIER, DEEP_ARCHIVE, etc.
- **Remembers the last upload time** to avoid duplicate uploads
- **Extracts EXIF metadata** (date, camera, GPS) for each photo (where available)
- **Handles duplicate files** (same EXIF date/name) gracefully
- **Reads all configuration from a YAML file**
- **Test mode**: Uploads only a configurable number of files for testing (see `test_mode_limit`)
- **Concurrent zipping and uploading** for faster performance (configurable with `max_concurrent_uploads`)
- **Configurable S3 key structure and log level**
- **Resume support**: If interrupted, resumes from the last successful month using `upload_state.json` (or `upload_state_test.json` in test mode)
- **Checksum verification**: Each uploaded zip is verified with a SHA256 checksum against the S3 object
- **Retry logic**: Failed uploads are retried up to 3 times before being marked as failed
- **Progress bar**: Shows upload progress in the terminal

---

## How It Works

1. **Finds all new media files** (by EXIF date or mod time) since the last successful upload
2. **Groups new files by year and month**, zips, and uploads to S3
3. **Each zip file is named** with the year, month, and a timestamp to avoid overwriting previous backups
4. **The last upload time is updated** only after all uploads complete successfully
5. **Tracks completed months** in `upload_state.json` (or `upload_state_test.json` for test mode) and skips them on future runs, allowing safe resumption after interruption
6. **Each upload is verified** by comparing the SHA256 checksum of the local zip and the S3 object
7. **If an upload fails**, it is retried up to 3 times before being marked as failed
8. **Test mode** uses the same logic, but uploads to a test folder and can be limited by `test_mode_limit`
9. **EXIF metadata** for all new files is saved to `photo_metadata.json` and uploaded to S3

---

## Project Structure

- `cmd/photos_backup.go`: Main Go program for full backup (concurrent, timestamped zips, resume, checksum)
- `cmd/testupload/main.go`: Test script to upload only a sample of files (configurable, timestamped zips, resume, checksum)
- `internal/photosbackup/photosbackup.go`: Shared library for backup logic (scanning, grouping, zipping, S3 upload, EXIF, checksums)
- `internal/photosbackup/upload_state.go`: Upload state tracking (resume support)
- `internal/photosbackup/photosbackup_test.go`: Unit tests for core logic
- `config.yaml`: Configuration file for S3 bucket, library path, etc.
- `go.mod`, `go.sum`: Go module and dependency files
- `last_upload.txt`: Stores the timestamp of the last successful upload (auto-created)
- `photo_metadata.json`: Metadata for all new files (auto-created)
- `upload_state.json` / `upload_state_test.json`: Tracks completed months for resume support

---

## Prerequisites

- Go 1.22 or later
- AWS account and S3 bucket
- AWS credentials configured (via `~/.aws/credentials`, environment variables, or IAM role)

---

## Configuration

Copy `config.sample.yaml` to `config.yaml` in the project root, then edit as needed. Example:

```yaml
# Sample configuration for AWS Photos Backup Utility
s3_bucket: your-s3-bucket-name
photos_library_path: /path/to/your/Photos Library.photoslibrary/originals
zip_file_name: photos_backup.zip
last_upload_file: last_upload.txt
s3_key_format: "{year}/{zip}"
log_level: "info"
region: us-east-1
test_mode_limit: 25
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
max_concurrent_uploads: 8  # Maximum number of concurrent zip/upload operations
```

**Key settings:**

- `s3_bucket`: Your S3 bucket name
- `photos_library_path`: Path to your Photos library originals
- `last_upload_file`: File to track last upload time
- `s3_key_format`: S3 key structure (default `{year}/{zip}`)
- `log_level`: Logging level (future use)
- `test_mode_limit`: Number of files to process in test mode (for test script)
- `storage_class`: S3 storage class for uploaded zips. Use `STANDARD` for regular S3, `GLACIER` or `DEEP_ARCHIVE` for archival storage.
- `allowed_extensions`: List of file extensions to include in backup. You can add or remove types as needed.
- `max_concurrent_uploads`: Maximum number of concurrent zip/upload operations (default: 8)

---

## Usage

### 1. Install dependencies

```sh
go mod tidy
```

### 2. Run the full backup (concurrent, resumable)

This will scan for all new photos and videos, group by year/month, zip, and upload to S3 concurrently:

```sh
go run ./cmd/photos_backup.go
```

### 3. Run a test upload (configurable number of files, resumable)

This will only upload a sample of new photos/videos for testing, using a `test/` prefix in S3. The number of files is set by `test_mode_limit` in your config:

```sh
go run ./cmd/testupload/main.go
```

### 4. Using VS Code Tasks

You can also run the full backup from the VS Code Command Palette:

- **Run Full Backup**: runs the main backup script

Open the Command Palette (`Cmd+Shift+P`), search for "Run Task", and select the task you want.

---

## Output Files

- `photo_metadata.json`: Metadata for all new files, uploaded to S3
- `last_upload.txt`: Tracks last successful upload time
- `upload_state.json` / `upload_state_test.json`: Tracks completed months for resume support
- Zipped archives: One per year/month, named with timestamp, deleted locally after upload

---

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

For more reliability on macOS, consider using a `launchd` plist. See [Apple's launchd documentation](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html).

---


## Notes

- The utility creates a `last_upload.txt` file in the project directory to track uploads
- AWS credentials must be available in your environment
- The utility only uploads new or modified files since the last run
- Zip files are deleted locally after upload
- S3 key structure, log level, and concurrency are configurable in `config.yaml`
- **Non-media files are skipped and a warning is logged**
- **EXIF metadata (date, camera, GPS) is extracted and stored in `photo_metadata.json`**
- **Duplicate files (same EXIF date/name) are detected and handled gracefully**
- **To reset or start the backup process over, you can safely delete the `upload_state.json` file. The next run will treat all months as not yet uploaded and re-upload everything as needed.**
- It is also recommended to delete `photo_metadata.json` when starting over, so a fresh metadata file is generated for the new backup set.

---

## Troubleshooting

- **No files are uploaded:**
  - Check that your `allowed_extensions` in `config.yaml` matches your actual file types
  - Ensure the `photos_library_path` is correct and accessible
  - Make sure `last_upload.txt` is not set to a future date
- **AWS upload errors:**
  - Verify your AWS credentials and permissions
  - Check your S3 bucket name and region in `config.yaml`
  - Ensure your network connection is stable
- **Permission denied errors:**
  - Run the tool with sufficient permissions to read your Photos library and write to the project directory
- **EXIF errors or missing dates:**
  - Some files may lack EXIF data; the tool will fall back to file modification time
- **Slow uploads or high resource usage:**
  - For large libraries, consider increasing available system resources or lowering `max_concurrent_uploads`

---

## Concurrency

By default, the tool launches up to `max_concurrent_uploads` goroutines for zipping and uploading. If you have many months with new files, this controls resource usage. Adjust `max_concurrent_uploads` in `config.yaml` as needed.

---

## License

MIT

# AWS Photos & Videos Backup Utility

This Go utility scans your macOS Photos library for new photos and videos, organizes them by year and month, zips each month's new files, and uploads the archives to an AWS S3 bucket. It is designed for regular (e.g., weekly) use, uploading only new files since the last backup. It supports full and test modes, resume, concurrency, and checksum verification.

---

## Features

- **Scans for new photos and videos** since the last upload, using EXIF date or file modification time
- **Configurable file types**: set in `allowed_extensions` in `config.yaml` (default includes most common photo/video formats)
- **Skips non-media files** and reports a summary of excluded file types after each run
- **Groups new files by year and month**
- **Zips each month's new files** into a separate archive with a unique timestamp (e.g., `2025-06_20250701T153000.zip`)
- **Uploads each zip file to S3** in a year-based folder (e.g., `2025/2025-06_20250701T153000.zip`)
- **Configurable S3 storage class**: STANDARD, GLACIER, DEEP_ARCHIVE, etc.
- **Remembers the last upload time** to avoid duplicate uploads
- **Extracts EXIF metadata** (date, camera, GPS) for each photo (where available)
- **Handles duplicate files** (same EXIF date/name) gracefully
- **Reads all configuration from a YAML file**
- **Test mode**: Uploads only a configurable number of files for testing (see `test_mode_limit`)
- **Concurrent zipping and uploading** for faster performance (configurable with `max_concurrent_uploads`)
- **Configurable S3 key structure and log level**
- **Resume support**: If interrupted, resumes from the last successful month using `upload_state.json` (or `upload_state_test.json` in test mode)
- **Checksum verification**: Each uploaded zip is verified with a SHA256 checksum against the S3 object
- **Retry logic**: Failed uploads are retried up to 3 times before being marked as failed
- **Progress bar**: Shows upload progress in the terminal

---

## How It Works

1. **Finds all new media files** (by EXIF date or mod time) since the last successful upload
2. **Groups new files by year and month**, zips, and uploads to S3
3. **Each zip file is named** with the year, month, and a timestamp to avoid overwriting previous backups
4. **The last upload time is updated** only after all uploads complete successfully
5. **Tracks completed months** in `upload_state.json` (or `upload_state_test.json` for test mode) and skips them on future runs, allowing safe resumption after interruption
6. **Each upload is verified** by comparing the SHA256 checksum of the local zip and the S3 object
7. **If an upload fails**, it is retried up to 3 times before being marked as failed
8. **Test mode** uses the same logic, but uploads to a test folder and can be limited by `test_mode_limit`
9. **EXIF metadata** for all new files is saved to `photo_metadata.json` and uploaded to S3

---

## Project Structure

- `cmd/photos_backup.go`: Main Go program for full backup (concurrent, timestamped zips, resume, checksum)
- `cmd/testupload/main.go`: Test script to upload only a sample of files (configurable, timestamped zips, resume, checksum)
- `internal/photosbackup/photosbackup.go`: Shared library for backup logic (scanning, grouping, zipping, S3 upload, EXIF, checksums)
- `internal/photosbackup/upload_state.go`: Upload state tracking (resume support)
- `internal/photosbackup/photosbackup_test.go`: Unit tests for core logic
- `config.yaml`: Configuration file for S3 bucket, library path, etc.
- `go.mod`, `go.sum`: Go module and dependency files
- `last_upload.txt`: Stores the timestamp of the last successful upload (auto-created)
- `photo_metadata.json`: Metadata for all new files (auto-created)
- `upload_state.json` / `upload_state_test.json`: Tracks completed months for resume support

---

## Prerequisites

- Go 1.22 or later
- AWS account and S3 bucket
- AWS credentials configured (via `~/.aws/credentials`, environment variables, or IAM role)

---

## Configuration


## Configuration

Copy `config.sample.yaml` to `config.yaml` in the project root, then edit as needed. Example:

```yaml
<<<<<<< HEAD
s3_bucket: your-s3-bucket-name
photos_library_path: /path/to/your/Photos\ Library.photoslibrary/originals
zip_file_name: photos_backup.zip  # (not used in grouped mode, but required)
last_upload_file: last_upload.txt
s3_key_format: "{year}/{zip}"
log_level: "info"
region: your-aws-region
test_mode_limit: 10  # Number of files to process in test mode
=======
# Sample configuration for AWS Photos Backup Utility
s3_bucket: your-s3-bucket-name
photos_library_path: /path/to/your/Photos Library.photoslibrary/originals
zip_file_name: photos_backup.zip
last_upload_file: last_upload.txt
s3_key_format: "{year}/{zip}"
log_level: "info"
region: us-east-1
test_mode_limit: 25
>>>>>>> todd
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
<<<<<<< HEAD
max_concurrent_uploads: 4  # Maximum number of concurrent zip/upload operations
=======
max_concurrent_uploads: 8  # Maximum number of concurrent zip/upload operations
>>>>>>> todd
```

**Key settings:**

- `s3_bucket`: Your S3 bucket name
- `photos_library_path`: Path to your Photos library originals
- `last_upload_file`: File to track last upload time
- `s3_key_format`: S3 key structure (default `{year}/{zip}`)
- `log_level`: Logging level (future use)
- `test_mode_limit`: Number of files to process in test mode (for test script)
- `storage_class`: S3 storage class for uploaded zips. Use `STANDARD` for regular S3, `GLACIER` or `DEEP_ARCHIVE` for archival storage.
- `allowed_extensions`: List of file extensions to include in backup. You can add or remove types as needed.
<<<<<<< HEAD
- `max_concurrent_uploads`: Maximum number of concurrent zip/upload operations. Adjust based on your system resources and network capacity.
=======
- `max_concurrent_uploads`: Maximum number of concurrent zip/upload operations (default: 8)

---
>>>>>>> todd

## Usage

### 1. Install dependencies

```sh
go mod tidy
```

### 2. Run the full backup (concurrent, resumable)

This will scan for all new photos and videos, group by year/month, zip, and upload to S3 concurrently:

```sh
go run ./cmd/photos_backup.go
```

### 3. Run a test upload (configurable number of files, resumable)

This will only upload a sample of new photos/videos for testing, using a `test/` prefix in S3. The number of files is set by `test_mode_limit` in your config:

```sh
go run ./cmd/testupload/main.go
```

### 4. Using VS Code Tasks

You can also run the full backup from the VS Code Command Palette:

- **Run Full Backup**: runs the main backup script

Open the Command Palette (`Cmd+Shift+P`), search for "Run Task", and select the task you want.

---

## Output Files

- `photo_metadata.json`: Metadata for all new files, uploaded to S3
- `last_upload.txt`: Tracks last successful upload time
- `upload_state.json` / `upload_state_test.json`: Tracks completed months for resume support
- Zipped archives: One per year/month, named with timestamp, deleted locally after upload

---

## Automating Weekly Backups

You can automate the utility to run weekly using `cron` or macOS `launchd`.

### Using cron

1. Open your crontab:
   ```sh
   crontab -e
   ```
2. Add a line to run the backup every Sunday at 2am:
   ```sh
   0 2 * * 0 cd /path/to/your/aws-photos-backup && /usr/local/go/bin/go run ./cmd/photos_backup.go
   ```
   - Adjust the path to `go` if needed (`which go` to find it).

### Using launchd (macOS)

For more reliability on macOS, consider using a `launchd` plist. See [Apple's launchd documentation](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html).

---


## Notes

- The utility creates a `last_upload.txt` file in the project directory to track uploads
- AWS credentials must be available in your environment
- The utility only uploads new or modified files since the last run
- Zip files are deleted locally after upload
- S3 key structure, log level, and concurrency are configurable in `config.yaml`
- **Non-media files are skipped and a warning is logged**
- **EXIF metadata (date, camera, GPS) is extracted and stored in `photo_metadata.json`**
- **Duplicate files (same EXIF date/name) are detected and handled gracefully**
- **To reset or start the backup process over, you can safely delete the `upload_state.json` file. The next run will treat all months as not yet uploaded and re-upload everything as needed.**
- It is also recommended to delete `photo_metadata.json` when starting over, so a fresh metadata file is generated for the new backup set.

---

## Troubleshooting

- **No files are uploaded:**
  - Check that your `allowed_extensions` in `config.yaml` matches your actual file types
  - Ensure the `photos_library_path` is correct and accessible
  - Make sure `last_upload.txt` is not set to a future date
- **AWS upload errors:**
  - Verify your AWS credentials and permissions
  - Check your S3 bucket name and region in `config.yaml`
  - Ensure your network connection is stable
- **Permission denied errors:**
  - Run the tool with sufficient permissions to read your Photos library and write to the project directory
- **EXIF errors or missing dates:**
  - Some files may lack EXIF data; the tool will fall back to file modification time
- **Slow uploads or high resource usage:**
  - For large libraries, consider increasing available system resources or lowering `max_concurrent_uploads`

---

## Concurrency

By default, the tool launches up to `max_concurrent_uploads` goroutines for zipping and uploading. If you have many months with new files, this controls resource usage. Adjust `max_concurrent_uploads` in `config.yaml` as needed.

---

## License

MIT


