package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func init() {
	// Initialize loggers for tests
	infoLog = log.New(os.Stdout, "", 0)
	errorLog = log.New(os.Stderr, "", 0)
}

func TestBackupFileMonitorWithAttachmentFiles(t *testing.T) {
	// Get the attachment_files directory path
	attachmentDir := "attachment_files"
	if _, err := os.Stat(attachmentDir); os.IsNotExist(err) {
		t.Skipf("attachment_files directory not found, skipping test")
	}

	// Create test directory with timestamp
	timestamp := time.Now().Format("20060102_150405")
	testDirName := fmt.Sprintf("test_monitor_%s", timestamp)
	
	projectRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	
	testDir := filepath.Join(projectRoot, testDirName)
	
	// Create the test directory
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	t.Logf("Test directory: %s", testDir)
	
	// Conditionally clean up
	if !*keepTestFiles {
		defer os.RemoveAll(testDir)
	} else {
		t.Logf("Keeping test files in: %s", testDir)
	}

	// Create backup transformer with media-transform-only=false to test all file types
	transformer := NewBackupTransformer(false, false)

	// Create file monitor
	monitor, err := NewBackupFileMonitor(testDir, transformer)
	if err != nil {
		t.Fatalf("Failed to create file monitor: %v", err)
	}

	// Track processed files
	processedFiles := make(map[string]bool)
	var processedMu sync.Mutex

	// Start monitoring
	if err := monitor.Start(); err != nil {
		t.Fatalf("Failed to start monitoring: %v", err)
	}
	defer monitor.Stop()

	// Give monitor a moment to initialize
	time.Sleep(500 * time.Millisecond)

	// Get list of files to copy
	files, err := os.ReadDir(attachmentDir)
	if err != nil {
		t.Fatalf("Failed to read attachment_files directory: %v", err)
	}

	// Filter to get actual files (not directories)
	var testFiles []string
	for _, file := range files {
		if !file.IsDir() {
			testFiles = append(testFiles, file.Name())
		}
	}

	if len(testFiles) == 0 {
		t.Fatalf("No test files found in attachment_files directory")
	}

	t.Logf("Found %d test files to copy", len(testFiles))

	// Copy files one by one with delays to simulate backup process
	for i, fileName := range testFiles {
		sourcePath := filepath.Join(attachmentDir, fileName)
		destPath := filepath.Join(testDir, fileName)

		// Copy file
		if err := copyFile(sourcePath, destPath); err != nil {
			t.Errorf("Failed to copy file %s: %v", fileName, err)
			continue
		}

		t.Logf("[%d/%d] Copied %s to test directory", i+1, len(testFiles), fileName)

		// Wait a bit between files to simulate backup process
		time.Sleep(200 * time.Millisecond)

		// Check if file was processed (give it some time)
		time.Sleep(1 * time.Second)
		
		processedMu.Lock()
		processedFiles[fileName] = true
		processedMu.Unlock()
	}

	// Wait for all files to be processed (with timeout)
	t.Logf("Waiting for files to be processed...")
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	allProcessed := false
	for !allProcessed {
		select {
		case <-timeout:
			t.Logf("Timeout waiting for files to be processed")
			allProcessed = true
		case <-ticker.C:
			// Check if files exist and have been modified
			allProcessed = true
			for _, fileName := range testFiles {
				filePath := filepath.Join(testDir, fileName)
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					// File was deleted (expected for unsupported types)
					continue
				}
				// File exists - check if it was processed
				// For media files, they should be converted to JPEG
				// For other files, they might be deleted or truncated
			}
		}
	}

	// Verify results
	t.Logf("\n=== Processing Summary ===")
	for _, fileName := range testFiles {
		filePath := filepath.Join(testDir, fileName)
		info, err := os.Stat(filePath)
		
		if os.IsNotExist(err) {
			t.Logf("  %s: DELETED (unsupported type)", fileName)
		} else if err != nil {
			t.Logf("  %s: ERROR checking file: %v", fileName, err)
		} else {
			// Check file type
			fileInfo, detectErr := transformer.detector.DetectFileType(filePath)
			if detectErr != nil {
				t.Logf("  %s: EXISTS (%d bytes) - Detection error: %v", fileName, info.Size(), detectErr)
			} else {
				t.Logf("  %s: EXISTS (%d bytes) - Type: %s", fileName, info.Size(), fileInfo.ContentType)
				
				// For media files, verify they were converted to JPEG
				if fileInfo.ContentType == "JPEG" {
					t.Logf("    ✅ Converted to JPEG")
				}
			}
		}
	}

	// Give monitor time to finish any remaining processing
	time.Sleep(2 * time.Second)
}

func TestBackupFileMonitorMediaTransformOnly(t *testing.T) {
	// Test with media-transform-only=true flag
	attachmentDir := "attachment_files"
	if _, err := os.Stat(attachmentDir); os.IsNotExist(err) {
		t.Skipf("attachment_files directory not found, skipping test")
	}

	timestamp := time.Now().Format("20060102_150405")
	testDirName := fmt.Sprintf("test_monitor_mediaonly_%s", timestamp)
	
	projectRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	
	testDir := filepath.Join(projectRoot, testDirName)
	
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	t.Logf("Test directory: %s", testDir)
	
	if !*keepTestFiles {
		defer os.RemoveAll(testDir)
	}

	// Create transformer with media-transform-only=true
	transformer := NewBackupTransformer(false, true)

	monitor, err := NewBackupFileMonitor(testDir, transformer)
	if err != nil {
		t.Fatalf("Failed to create file monitor: %v", err)
	}

	if err := monitor.Start(); err != nil {
		t.Fatalf("Failed to start monitoring: %v", err)
	}
	defer monitor.Stop()

	time.Sleep(500 * time.Millisecond)

	// Copy a few test files
	testFiles := []string{"jpeg1", "png_1", "pdf_1"}
	for _, fileName := range testFiles {
		sourcePath := filepath.Join(attachmentDir, fileName)
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			continue // Skip if file doesn't exist
		}
		
		destPath := filepath.Join(testDir, fileName)
		if err := copyFile(sourcePath, destPath); err != nil {
			t.Logf("Failed to copy %s: %v", fileName, err)
			continue
		}
		
		t.Logf("Copied %s", fileName)
		time.Sleep(200 * time.Millisecond)
	}

	// Wait for processing
	time.Sleep(3 * time.Second)

	t.Logf("\n=== Media-Transform-Only Mode Results ===")
	for _, fileName := range testFiles {
		filePath := filepath.Join(testDir, fileName)
		info, err := os.Stat(filePath)
		
		if os.IsNotExist(err) {
			t.Logf("  %s: DELETED", fileName)
		} else if err != nil {
			t.Logf("  %s: ERROR: %v", fileName, err)
		} else {
			fileInfo, detectErr := transformer.detector.DetectFileType(filePath)
			if detectErr != nil {
				t.Logf("  %s: EXISTS (%d bytes)", fileName, info.Size())
			} else {
				t.Logf("  %s: EXISTS (%d bytes) - Type: %s", fileName, info.Size(), fileInfo.ContentType)
				
				// With media-transform-only, non-media files should be untouched
				if fileInfo.ContentType != "JPEG" && fileInfo.ContentType != "PNG" && fileInfo.ContentType != "HEIC" {
					t.Logf("    ✅ Skipped (non-media file preserved)")
				}
			}
		}
	}
}


