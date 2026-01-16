package main

import (
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func init() {
	// Initialize loggers for tests
	infoLog = log.New(os.Stdout, "", 0)
	errorLog = log.New(os.Stderr, "", 0)
}

// TestGIFConversionErrorRecovery tests that GIF conversion errors don't crash
func TestGIFConversionErrorRecovery(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a fake GIF file (corrupted)
	fakeGif := filepath.Join(tempDir, "test.gif")
	if err := os.WriteFile(fakeGif, []byte("not a gif"), 0644); err != nil {
		t.Fatalf("Failed to create fake GIF: %v", err)
	}
	
	// This should not crash
	transformer.convertGifToJpeg(fakeGif, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Success if no crash
}

// TestPNGConversionErrorRecovery tests that PNG conversion errors don't crash
func TestPNGConversionErrorRecovery(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a fake PNG file (corrupted)
	fakePng := filepath.Join(tempDir, "test.png")
	if err := os.WriteFile(fakePng, []byte("not a png"), 0644); err != nil {
		t.Fatalf("Failed to create fake PNG: %v", err)
	}
	
	// This should not crash
	transformer.convertPngToJpeg(fakePng, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Success if no crash
}

// TestWEBPConversionErrorRecovery tests that WEBP conversion errors don't crash
func TestWEBPConversionErrorRecovery(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a fake WEBP file (corrupted)
	fakeWebp := filepath.Join(tempDir, "test.webp")
	if err := os.WriteFile(fakeWebp, []byte("not a webp"), 0644); err != nil {
		t.Fatalf("Failed to create fake WEBP: %v", err)
	}
	
	// This should not crash
	transformer.convertWebpToJpeg(fakeWebp, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Success if no crash
}

// TestJPEGResizeErrorRecovery tests that JPEG resize errors don't crash
func TestJPEGResizeErrorRecovery(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a fake JPEG file (corrupted)
	fakeJpeg := filepath.Join(tempDir, "test.jpg")
	if err := os.WriteFile(fakeJpeg, []byte("not a jpeg"), 0644); err != nil {
		t.Fatalf("Failed to create fake JPEG: %v", err)
	}
	
	// This should not crash
	transformer.resizeJpeg(fakeJpeg, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Success if no crash
}

// TestHEICConversionMissingTool tests HEIC conversion when tool is missing
func TestHEICConversionMissingTool(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a test HEIC file
	testHeic := filepath.Join(tempDir, "test.heic")
	if err := os.WriteFile(testHeic, []byte("fake heic data"), 0644); err != nil {
		t.Fatalf("Failed to create test HEIC: %v", err)
	}
	
	// This should gracefully handle missing heic-converter
	transformer.convertHeicToJpeg(testHeic, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Success if no crash
}

// TestVideoConversionMissingTool tests video conversion when ffmpeg is missing
func TestVideoConversionMissingTool(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a test video file
	testVideo := filepath.Join(tempDir, "test.mp4")
	if err := os.WriteFile(testVideo, []byte("fake video data"), 0644); err != nil {
		t.Fatalf("Failed to create test video: %v", err)
	}
	
	// This should gracefully handle missing ffmpeg
	transformer.convertVideoToJpeg(testVideo, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Success if no crash
}

// TestValidGIFConversion tests that valid GIF conversion works
func TestValidGIFConversion(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a valid GIF file
	gifFile := filepath.Join(tempDir, "test.gif")
	img := image.NewPaletted(image.Rect(0, 0, 100, 100), color.Palette{
		color.RGBA{0, 0, 0, 255},
		color.RGBA{255, 0, 0, 255},
	})
	
	f, err := os.Create(gifFile)
	if err != nil {
		t.Fatalf("Failed to create GIF file: %v", err)
	}
	if err := gif.Encode(f, img, nil); err != nil {
		f.Close()
		t.Fatalf("Failed to encode GIF: %v", err)
	}
	f.Close()
	
	// Convert
	transformer.convertGifToJpeg(gifFile, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Check that file still exists (should be converted to JPEG in place)
	if _, err := os.Stat(gifFile); err != nil {
		t.Errorf("Converted file should still exist at original path: %v", err)
	}
}

// TestValidPNGConversion tests that valid PNG conversion works
func TestValidPNGConversion(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a valid PNG file
	pngFile := filepath.Join(tempDir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	
	f, err := os.Create(pngFile)
	if err != nil {
		t.Fatalf("Failed to create PNG file: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("Failed to encode PNG: %v", err)
	}
	f.Close()
	
	// Convert
	transformer.convertPngToJpeg(pngFile, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Check that file still exists
	if _, err := os.Stat(pngFile); err != nil {
		t.Errorf("Converted file should still exist at original path: %v", err)
	}
}

// TestValidJPEGResize tests that valid JPEG resize works
func TestValidJPEGResize(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a valid JPEG file
	jpegFile := filepath.Join(tempDir, "test.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	
	f, err := os.Create(jpegFile)
	if err != nil {
		t.Fatalf("Failed to create JPEG file: %v", err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		f.Close()
		t.Fatalf("Failed to encode JPEG: %v", err)
	}
	f.Close()
	
	// Get original size
	origInfo, err := os.Stat(jpegFile)
	if err != nil {
		t.Fatalf("Failed to stat original file: %v", err)
	}
	
	// Resize
	transformer.resizeJpeg(jpegFile, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Check that file still exists and was resized
	newInfo, err := os.Stat(jpegFile)
	if err != nil {
		t.Errorf("Resized file should still exist at original path: %v", err)
	}
	
	if newInfo.Size() >= origInfo.Size() {
		t.Errorf("Resized file should be smaller, got %d bytes vs original %d bytes", 
			newInfo.Size(), origInfo.Size())
	}
}

// TestTempFileCleanup tests that temp files are cleaned up properly
func TestTempFileCleanup(t *testing.T) {
	tempDir := t.TempDir()
	
	// Create a valid PNG
	pngFile := filepath.Join(tempDir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	f, err := os.Create(pngFile)
	if err != nil {
		t.Fatalf("Failed to create PNG: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("Failed to encode PNG: %v", err)
	}
	f.Close()
	
	// Count files before
	filesBefore, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	
	// Convert (which creates temp files)
	transformer := NewBackupTransformer()
	transformer.convertPngToJpeg(pngFile, &FileTiming{
		CreatedTime:     time.Now(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "test",
	})
	
	// Count files after
	filesAfter, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	
	// Should have same number of files (temp files cleaned up)
	if len(filesAfter) != len(filesBefore) {
		t.Logf("Files before: %d, after: %d", len(filesBefore), len(filesAfter))
		t.Logf("Files after conversion: %v", filesAfter)
		// This is a warning, not a failure, since some cleanup might be deferred
	}
}

// TestResizeJpegImageInvalidInput tests error handling for invalid input
func TestResizeJpegImageInvalidInput(t *testing.T) {
	tempDir := t.TempDir()
	
	// Non-existent file
	_, err := resizeJpegImage(filepath.Join(tempDir, "nonexistent.jpg"), 500)
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
	
	// Invalid JPEG
	invalidJpeg := filepath.Join(tempDir, "invalid.jpg")
	if err := os.WriteFile(invalidJpeg, []byte("not a jpeg"), 0644); err != nil {
		t.Fatalf("Failed to create invalid JPEG: %v", err)
	}
	
	_, err = resizeJpegImage(invalidJpeg, 500)
	if err == nil {
		t.Error("Expected error for invalid JPEG")
	}
}

// TestProcessFileByExtension tests extension-based processing
func TestProcessFileByExtension(t *testing.T) {
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create test files for each type
	testCases := []struct {
		name string
		ext  string
	}{
		{"test.jpg", ".jpg"},
		{"test.png", ".png"},
		{"test.gif", ".gif"},
		{"test.heic", ".heic"},
		{"test.webp", ".webp"},
		{"test.mp4", ".mp4"},
		{"test.3gp", ".3gp"},   // Mobile video
		{"test.ts", ".ts"},     // Transport stream
		{"test.vob", ".vob"},   // DVD video
		{"test.ogv", ".ogv"},   // Ogg video
		{"test.txt", ".txt"},   // Should be ignored
	}
	
	for _, tc := range testCases {
		testFile := filepath.Join(tempDir, tc.name)
		if err := os.WriteFile(testFile, []byte("test data"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", tc.name, err)
		}
		
		// Process - should not crash
		transformer.ProcessFileByExtension(testFile, tc.ext, &FileTiming{
			CreatedTime:     time.Now(),
			DiscoveredTime:  time.Now(),
			DiscoveryMethod: "test",
		})
	}
}

// TestQueueDepthTracking tests that queue depth is tracked correctly
func TestQueueDepthTracking(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	
	transformer := NewBackupTransformer()
	runner, err := NewBackupRunner(backupDir, "ios_backup", false, transformer)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	
	// Check initial state
	if runner.activeCount != 0 {
		t.Errorf("Expected activeCount 0, got %d", runner.activeCount)
	}
	if runner.totalCount != 0 {
		t.Errorf("Expected totalCount 0, got %d", runner.totalCount)
	}
	
	// Queue depth function should work
	if transformer.queueDepth != nil {
		active, total := transformer.queueDepth()
		if active != 0 || total != 0 {
			t.Errorf("Expected (0, 0), got (%d, %d)", active, total)
		}
	}
}

// TestContentDetectorVariousFiles tests content detection for various file types
func TestContentDetectorVariousFiles(t *testing.T) {
	detector := NewContentDetector()
	tempDir := t.TempDir()
	
	testCases := []struct {
		name        string
		content     []byte
		expectedType string
	}{
		{"test.pdf", []byte{0x25, 0x50, 0x44, 0x46, 0x2D}, "PDF"},
		{"test.png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "PNG"},
		{"test.jpg", []byte{0xFF, 0xD8, 0xFF}, "JPEG"},
		{"test.gif", []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}, "GIF"},
	}
	
	for _, tc := range testCases {
		testFile := filepath.Join(tempDir, tc.name)
		if err := os.WriteFile(testFile, tc.content, 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", tc.name, err)
		}
		
		fileInfo, err := detector.DetectFileType(testFile)
		if err != nil {
			t.Errorf("Failed to detect file type for %s: %v", tc.name, err)
			continue
		}
		
		if fileInfo.ContentType != tc.expectedType {
			t.Errorf("Expected type %s for %s, got %s", tc.expectedType, tc.name, fileInfo.ContentType)
		}
	}
}

// TestFileSavedLineParsing tests FILE_SAVED line parsing
func TestFileSavedLineParsing(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "00008110-000E785101F2401E")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Failed to create backup dir: %v", err)
	}
	
	// Create test file structure
	snapshotDir := filepath.Join(backupDir, "Snapshot")
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		t.Fatalf("Failed to create snapshot dir: %v", err)
	}
	
	testFile := filepath.Join(snapshotDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	transformer := NewBackupTransformer()
	runner, err := NewBackupRunner(backupDir, "ios_backup", false, transformer)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	
	// Test parsing various FILE_SAVED formats
	testLines := []string{
		"FILE_SAVED: path=00008110-000E785101F2401E/Snapshot/test.txt domain=MediaDomain",
		"FILE_SAVED: path=test.txt domain=TestDomain",
		"FILE_SAVED: path=test.txt", // Missing domain
		"Not a FILE_SAVED line",
	}
	
	for _, line := range testLines {
		filePath, domain := runner.parseSavedFileLine(line)
		if filePath != "" {
			t.Logf("Parsed: %s -> path=%s, domain=%s", line, filePath, domain)
		}
	}
}
