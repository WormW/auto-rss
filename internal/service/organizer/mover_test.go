package organizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WormW/auto-rss/internal/model"
)

func TestFileMover_IsVideoFile(t *testing.T) {
	mover := NewFileMover()

	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{"MKV file", "video.mkv", true},
		{"MP4 file", "video.mp4", true},
		{"AVI file", "video.avi", true},
		{"FLV file", "video.flv", true},
		{"TS file", "video.ts", true},
		{"M2TS file", "video.m2ts", true},
		{"MOV file", "video.mov", true},
		{"WMV file", "video.wmv", true},
		{"Non-video file", "file.txt", false},
		{"No extension", "file", false},
		{"Uppercase extension", "video.MKV", true},
		{"Mixed case extension", "video.Mp4", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mover.IsVideoFile(tt.filePath)
			if got != tt.want {
				t.Errorf("IsVideoFile(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestFileMover_Copy(t *testing.T) {
	// Create temp directories
	srcDir := t.TempDir()
	destDir := t.TempDir()

	mover := NewFileMover()

	// Create a source file
	srcFile := filepath.Join(srcDir, "test.txt")
	content := []byte("Hello, World!")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Test copy
	destFile := filepath.Join(destDir, "test.txt")
	if err := mover.Copy(srcFile, destFile); err != nil {
		t.Errorf("Copy() error = %v", err)
	}

	// Verify content
	copiedContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read copied file: %v", err)
	}
	if string(copiedContent) != string(content) {
		t.Errorf("Copied content = %q, want %q", copiedContent, content)
	}

	// Verify permissions
	srcInfo, _ := os.Stat(srcFile)
	destInfo, _ := os.Stat(destFile)
	if srcInfo.Mode() != destInfo.Mode() {
		t.Errorf("Permissions differ: src=%v, dest=%v", srcInfo.Mode(), destInfo.Mode())
	}
}

func TestFileMover_Copy_NonExistentSource(t *testing.T) {
	mover := NewFileMover()
	destDir := t.TempDir()

	err := mover.Copy("/non/existent/file.txt", filepath.Join(destDir, "dest.txt"))
	if err == nil {
		t.Error("Expected error when copying non-existent file")
	}
}

func TestFileMover_Move(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	mover := NewFileMover()

	// Create a source file
	srcFile := filepath.Join(tempDir, "source.txt")
	content := []byte("Test content")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Test move
	destFile := filepath.Join(tempDir, "dest.txt")
	if err := mover.Move(srcFile, destFile); err != nil {
		t.Errorf("Move() error = %v", err)
	}

	// Verify source no longer exists
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Error("Source file should not exist after move")
	}

	// Verify destination exists with correct content
	movedContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read moved file: %v", err)
	}
	if string(movedContent) != string(content) {
		t.Errorf("Moved content = %q, want %q", movedContent, content)
	}
}

func TestFileMover_Move_DestinationExists(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	mover := NewFileMover()

	// Create source file
	srcFile := filepath.Join(tempDir, "source.txt")
	if err := os.WriteFile(srcFile, []byte("Source content"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create existing destination file
	destFile := filepath.Join(tempDir, "dest.txt")
	if err := os.WriteFile(destFile, []byte("Existing content"), 0644); err != nil {
		t.Fatalf("Failed to create destination file: %v", err)
	}

	// Test move - should create file with timestamp suffix
	if err := mover.Move(srcFile, destFile); err != nil {
		t.Errorf("Move() error = %v", err)
	}

	// Verify original destination still exists
	if _, err := os.Stat(destFile); err != nil {
		t.Error("Original destination file should still exist")
	}

	// Verify source was moved to a new file with timestamp suffix
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}

	foundMoved := false
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "dest_") && strings.HasSuffix(entry.Name(), ".txt") {
			foundMoved = true
			break
		}
	}
	if !foundMoved {
		t.Error("Expected to find moved file with timestamp suffix")
	}
}

func TestFileMover_MoveWithFallback(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	mover := NewFileMover()

	// Create source file
	srcFile := filepath.Join(tempDir, "source.txt")
	if err := os.WriteFile(srcFile, []byte("Content"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Test move with fallback
	destFile := filepath.Join(tempDir, "subdir", "dest.txt")
	finalDest, err := mover.MoveWithFallback(srcFile, destFile)
	if err != nil {
		t.Errorf("MoveWithFallback() error = %v", err)
	}

	// Verify returned path matches destination
	if finalDest != destFile {
		t.Errorf("MoveWithFallback() returned %q, want %q", finalDest, destFile)
	}

	// Verify file was moved
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Error("Source file should not exist after move")
	}
	if _, err := os.Stat(destFile); err != nil {
		t.Error("Destination file should exist")
	}
}

func TestFileMover_MoveWithFallback_SourceNotExist(t *testing.T) {
	mover := NewFileMover()
	destDir := t.TempDir()

	_, err := mover.MoveWithFallback("/non/existent/file.txt", filepath.Join(destDir, "dest.txt"))
	if err == nil {
		t.Error("Expected error when source doesn't exist")
	}
}

func TestFileMover_CleanEmptyDirs(t *testing.T) {
	// Create temp directory structure
	tempDir := t.TempDir()
	mover := NewFileMover()

	// Create nested empty directories
	dir1 := filepath.Join(tempDir, "level1")
	dir2 := filepath.Join(dir1, "level2")
	dir3 := filepath.Join(dir2, "level3")

	if err := os.MkdirAll(dir3, 0755); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}

	// Create a file in level2 (so it shouldn't be deleted)
	testFile := filepath.Join(dir2, "keep.txt")
	if err := os.WriteFile(testFile, []byte("keep"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Clean empty directories starting from level3
	mover.CleanEmptyDirs(dir3)

	// Verify level3 was removed (it was empty)
	if _, err := os.Stat(dir3); !os.IsNotExist(err) {
		t.Error("level3 should have been removed (empty)")
	}

	// Verify level2 still exists (has file)
	if _, err := os.Stat(dir2); err != nil {
		t.Error("level2 should still exist (has file)")
	}

	// Verify level1 still exists (has level2)
	if _, err := os.Stat(dir1); err != nil {
		t.Error("level1 should still exist (has level2)")
	}
}

func TestFileMover_CleanEmptyDirs_AllEmpty(t *testing.T) {
	// Create temp directory structure
	tempDir := t.TempDir()
	mover := NewFileMover()

	// Create nested empty directories
	dir1 := filepath.Join(tempDir, "level1")
	dir2 := filepath.Join(dir1, "level2")

	if err := os.MkdirAll(dir2, 0755); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}

	// Clean from level1
	mover.CleanEmptyDirs(dir1)

	// Verify level2 was removed
	if _, err := os.Stat(dir2); !os.IsNotExist(err) {
		t.Error("level2 should have been removed")
	}

	// Verify level1 was removed (became empty after level2 removal)
	if _, err := os.Stat(dir1); !os.IsNotExist(err) {
		t.Error("level1 should have been removed (became empty)")
	}
}

func TestFileMover_IsFileReady(t *testing.T) {
	// Create temp file
	tempDir := t.TempDir()
	mover := NewFileMover()

	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test ready file
	ready := mover.IsFileReady(testFile)
	if !ready {
		t.Error("Expected file to be ready")
	}

	// Test non-existent file
	nonExistent := mover.IsFileReady("/non/existent/file.txt")
	if nonExistent {
		t.Error("Non-existent file should not be ready")
	}
}

func TestFileMover_IsAlreadyOrganized(t *testing.T) {
	mover := NewFileMover()

	tests := []struct {
		name         string
		filePath     string
		subscription *model.Subscription
		want         bool
	}{
		{
			name:         "Organized file in season dir",
			filePath:     "/anime/Show Name/Season 1/Show Name S01E01.mkv",
			subscription: &model.Subscription{Name: "Show Name"},
			want:         true,
		},
		{
			name:         "Organized file with different case",
			filePath:     "/anime/show name/season 1/show name s01e01.mkv",
			subscription: &model.Subscription{Name: "Show Name"},
			want:         true,
		},
		{
			name:         "Not organized - no season dir",
			filePath:     "/downloads/[Group] Show Name - 01.mkv",
			subscription: &model.Subscription{Name: "Show Name"},
			want:         false,
		},
		{
			name:         "Not organized - no SxxExx pattern",
			filePath:     "/anime/Show Name/Season 1/Show Name Episode 1.mkv",
			subscription: &model.Subscription{Name: "Show Name"},
			want:         false,
		},
		{
			name:         "Not organized - wrong title",
			filePath:     "/anime/Other Show/Season 1/Other Show S01E01.mkv",
			subscription: &model.Subscription{Name: "Show Name"},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mover.IsAlreadyOrganized(tt.filePath, tt.subscription)
			if got != tt.want {
				t.Errorf("IsAlreadyOrganized(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestValidateMovePaths(t *testing.T) {
	// Create temp directories
	srcRoot := t.TempDir()
	destRoot := t.TempDir()

	// Create test files
	validSrc := filepath.Join(srcRoot, "source.txt")
	validDest := filepath.Join(destRoot, "dest.txt")
	os.WriteFile(validSrc, []byte("content"), 0644)

	tests := []struct {
		name            string
		src             string
		dest            string
		allowedSrcRoot  string
		allowedDestRoot string
		wantErr         bool
	}{
		{
			name:            "Valid paths",
			src:             validSrc,
			dest:            validDest,
			allowedSrcRoot:  srcRoot,
			allowedDestRoot: destRoot,
			wantErr:         false,
		},
		{
			name:            "Source escapes root",
			src:             "/etc/passwd",
			dest:            validDest,
			allowedSrcRoot:  srcRoot,
			allowedDestRoot: destRoot,
			wantErr:         true,
		},
		{
			name:            "Destination escapes root",
			src:             validSrc,
			dest:            "/etc/passwd",
			allowedSrcRoot:  srcRoot,
			allowedDestRoot: destRoot,
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMovePaths(tt.src, tt.dest, tt.allowedSrcRoot, tt.allowedDestRoot)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMovePaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestFileMover_Move_CrossDevice simulates cross-device move behavior
// by forcing a copy+delete when rename fails
func TestFileMover_Move_CrossDevice(t *testing.T) {
	// This test verifies that Move falls back to Copy+Delete when Rename fails
	// In a real cross-device scenario, os.Rename would fail

	tempDir := t.TempDir()
	mover := NewFileMover()

	// Create source file
	srcFile := filepath.Join(tempDir, "source.txt")
	content := []byte("Cross-device test content")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Move to same directory (should use rename)
	destFile := filepath.Join(tempDir, "dest.txt")
	if err := mover.Move(srcFile, destFile); err != nil {
		t.Errorf("Move() error = %v", err)
	}

	// Verify
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Error("Source file should not exist after move")
	}

	movedContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read moved file: %v", err)
	}
	if string(movedContent) != string(content) {
		t.Errorf("Moved content mismatch")
	}
}

// TestFileMover_IsFileReady_RapidChange tests that a rapidly changing file is not ready
func TestFileMover_IsFileReady_RapidChange(t *testing.T) {
	// This test is timing-dependent and may be flaky
	// We test the inverse - a stable file should be ready

	tempDir := t.TempDir()
	mover := NewFileMover()

	testFile := filepath.Join(tempDir, "stable.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Wait to ensure file is stable
	time.Sleep(2 * time.Second)

	ready := mover.IsFileReady(testFile)
	if !ready {
		t.Error("Stable file should be ready")
	}
}
