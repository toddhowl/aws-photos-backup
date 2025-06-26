package photosbackup

import (
	"os"
	"testing"
	"time"
)

func TestGetLastUploadTimeAndUpdate(t *testing.T) {
	file := "test_last_upload.txt"
	defer os.Remove(file)
	UpdateLastUploadTime(file)
	t1 := GetLastUploadTime(file)
	if time.Since(t1) > time.Minute {
		t.Errorf("Last upload time not updated correctly: got %v", t1)
	}
}

func TestFindNewPhotosFiltersByExtension(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/a.jpg", []byte("test"), 0644)
	os.WriteFile(dir+"/b.txt", []byte("test"), 0644)
	allowed := []string{".jpg"}
	files, excluded := FindNewPhotos(dir, time.Time{}, allowed)
	if len(files) != 1 || excluded[".txt"] != 1 {
		t.Errorf("Expected 1 jpg and 1 excluded txt, got files=%v, excluded=%v", files, excluded)
	}
}

func TestGroupPhotosByYearMonth(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/a.jpg"
	os.WriteFile(file, []byte("test"), 0644)
	old := time.Now().AddDate(-1, 0, 0)
	os.Chtimes(file, old, old)
	files := []string{file}
	groups := GroupPhotosByYearMonth(files)
	if len(groups) == 0 {
		t.Errorf("Expected at least one group, got %v", groups)
	}
}
