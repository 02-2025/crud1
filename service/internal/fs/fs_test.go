package fs

import (
	"os"
	"path/filepath"
	"testing"
)


func TestExistenceCheck(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test")

	if _, err := os.Create(filePath); err != nil {
		t.Fatal(err)
	}

	if !ExistenceCheck(filePath) {
		t.Error("file should exist")
	}

	if ExistenceCheck(filepath.Join(tempDir, "missing")) {
		t.Error("file should not exist")
	}
}

func TestPathGuard(t *testing.T) {
	root := "/tmp/root"

	path, err := PathGuard(root, "/../..//etc/passwd")
	if err != nil {
		t.Fatal(err)
	}

	expected := filepath.Join(root, "/etc/passwd")

	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestSublevelCheck(t *testing.T) {
	src := "/home/user/docs"
	dst := "/home/user/docs/subdir"

	if !SublevelCheck(src, dst) {
		t.Error("dst should be inside src")
	}
}

func TestWrite(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "write.txt")

	if err := os.WriteFile(filePath, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Write(filePath, "new content", false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "new content" {
		t.Errorf("unexpected content: %s", string(data))
	}
}

func TestGetFileStats(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "stats.txt")

	text := "hello world\nhello go"

	if err := os.WriteFile(filePath, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := GetFileStats(filePath)
	if err != nil {
		t.Fatal(err)
	}

	if stats.Words != 4 {
		t.Errorf("expected 4 words, got %d", stats.Words)
	}

	if stats.UniqueWords != 3 {
		t.Errorf("expected 3 unique words, got %d", stats.UniqueWords)
	}
}
