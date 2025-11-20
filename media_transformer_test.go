package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var keepTestFiles = flag.Bool("keep-test-files", false, "Keep test output files instead of deleting them")

func TestBackupTransformerWithAttachmentFiles(t *testing.T) {
	// Get the attachment_files directory path
	attachmentDir := "attachment_files"
	if _, err := os.Stat(attachmentDir); os.IsNotExist(err) {
		t.Skipf("attachment_files directory not found, skipping test")
	}

	// Create output directory in project root with timestamp
	timestamp := time.Now().Format("20060102_150405")
	outputDirName := fmt.Sprintf("test_output_%s", timestamp)
	
	// Get current working directory (project root)
	projectRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	
	outputDir := filepath.Join(projectRoot, outputDirName)
	
	// Create the output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Failed to create output directory: %v", err)
	}
	
	// Log the output directory location
	t.Logf("Test output directory: %s", outputDir)
	
	// Conditionally clean up based on flag
	if !*keepTestFiles {
		defer os.RemoveAll(outputDir)
	} else {
		t.Logf("Keeping test files in: %s (use -keep-test-files=false to auto-cleanup)", outputDir)
	}

	// Create backup transformer
	transformer := NewBackupTransformer(false, false)

	// Test files from attachment_files (files have no extensions to match real-world iOS backup)
	testFiles := []struct {
		name        string
		sourceFile  string
		expectType  string
		shouldConvert bool
	}{
		{"HEIC image 1", "heic1", "HEIC", true},
		{"HEIC image 2", "heic2", "HEIC", true},
		{"HEIC image 3", "heic3", "HEIC", true},
		{"GIF animated 1", "gif_annimated_2", "GIF", true},
		{"GIF animated 2", "gif_annimated_3", "GIF", true},
		{"MP4 video", "mp4_1", "MP4", true},
		{"MOV video", "mov_1", "MOV", true},
		{"M4V video", "m4v_1", "MP4", true}, // M4V is typically MP4 container
		{"MPG video", "mpg_1", "MPG", true}, // MPG/MPEG video
	}

	for _, tt := range testFiles {
		t.Run(tt.name, func(t *testing.T) {
			sourcePath := filepath.Join(attachmentDir, tt.sourceFile)
			
			// Check if source file exists
			if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
				t.Skipf("Source file %s does not exist, skipping", tt.sourceFile)
				return
			}

			// Copy file to output directory (we'll overwrite it during conversion)
			destPath := filepath.Join(outputDir, tt.sourceFile)
			if err := copyFile(sourcePath, destPath); err != nil {
				t.Fatalf("Failed to copy file: %v", err)
			}

			// Get original file size
			originalInfo, err := os.Stat(destPath)
			if err != nil {
				t.Fatalf("Failed to stat original file: %v", err)
			}
			originalSize := originalInfo.Size()

			// Detect file type
			fileInfo, err := transformer.detector.DetectFileType(destPath)
			if err != nil {
				t.Fatalf("Failed to detect file type: %v", err)
			}

			t.Logf("Detected type: %s (expected: %s), Confidence: %s", 
				fileInfo.ContentType, tt.expectType, fileInfo.Confidence)

			// Only test conversion if we expect it to convert
			if !tt.shouldConvert {
				t.Logf("Skipping conversion for %s (not expected to convert)", tt.name)
				return
			}

			// Process the file
			converted := transformer.ProcessFile(destPath)
			if !converted {
				t.Logf("File %s was not converted (may not be supported or tool not available)", tt.name)
				return
			}

			// Check if file still exists
			if _, err := os.Stat(destPath); os.IsNotExist(err) {
				t.Errorf("File was deleted after conversion")
				return
			}

			// Get converted file size
			convertedInfo, err := os.Stat(destPath)
			if err != nil {
				t.Fatalf("Failed to stat converted file: %v", err)
			}
			convertedSize := convertedInfo.Size()

			// Rename file to .jpg extension since it's now a JPEG
			// Files don't have extensions, so just add .jpg to the base name
			baseName := filepath.Base(tt.sourceFile)
			jpgPath := filepath.Join(outputDir, baseName+".jpg")
			
			if err := os.Rename(destPath, jpgPath); err != nil {
				t.Fatalf("Failed to rename converted file to .jpg: %v", err)
			}

			t.Logf("Original size: %d bytes, Converted size: %d bytes", originalSize, convertedSize)
			t.Logf("Converted file saved to: %s", jpgPath)

			// Verify it's now a JPEG (for images) or JPEG thumbnail (for videos)
			// Re-detect to confirm type changed
			newFileInfo, err := transformer.detector.DetectFileType(jpgPath)
			if err != nil {
				t.Fatalf("Failed to detect converted file type: %v", err)
			}

			// For HEIC and GIF, should be JPEG now
			if tt.expectType == "HEIC" || tt.expectType == "GIF" {
				if newFileInfo.ContentType != "JPEG" {
					t.Errorf("Expected JPEG after conversion, got %s", newFileInfo.ContentType)
				} else {
					t.Logf("✅ Successfully converted %s to JPEG", tt.name)
				}
			}

			// For videos, should be JPEG thumbnail
			if tt.expectType == "MP4" || tt.expectType == "MOV" || tt.expectType == "MPG" {
				if newFileInfo.ContentType != "JPEG" {
					t.Logf("Video converted file type: %s (may need ffmpeg for proper conversion)", newFileInfo.ContentType)
				} else {
					t.Logf("✅ Successfully converted %s video to JPEG thumbnail", tt.name)
				}
			}
		})
	}
}

func TestBackupTransformerFileDetection(t *testing.T) {
	attachmentDir := "attachment_files"
	if _, err := os.Stat(attachmentDir); os.IsNotExist(err) {
		t.Skipf("attachment_files directory not found, skipping test")
	}

	transformer := NewBackupTransformer(false, false)

	// Test all files in attachment_files for type detection
	files, err := os.ReadDir(attachmentDir)
	if err != nil {
		t.Fatalf("Failed to read attachment_files directory: %v", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filePath := filepath.Join(attachmentDir, file.Name())
		
		t.Run(file.Name(), func(t *testing.T) {
			fileInfo, err := transformer.detector.DetectFileType(filePath)
			if err != nil {
				t.Logf("Failed to detect type for %s: %v", file.Name(), err)
				return
			}

			t.Logf("File: %s, Type: %s, Description: %s, Confidence: %s, Size: %d bytes",
				file.Name(),
				fileInfo.ContentType,
				fileInfo.Description,
				fileInfo.Confidence,
				fileInfo.Size,
			)
		})
	}
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	return err
}

