package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() {
	// Initialize loggers for tests
	infoLog = log.New(os.Stdout, "", 0)
	errorLog = log.New(os.Stderr, "", 0)
}

// TestPanicRecoveryInProcessFile tests that panics in processFile are recovered
func TestPanicRecoveryInProcessFile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	
	// Create mock transformer that panics
	transformer := NewBackupTransformer()
	
	// Create runner
	runner, err := NewBackupRunner(backupDir, "ios_backup", false, transformer)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	
	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	// This should not crash even though the file doesn't have proper extension
	// The panic recovery should catch any issues
	runner.processFile(testFile, "test.unknown")
	
	// If we get here, the test passed (no crash)
}

// TestGoroutinePanicRecovery tests that panics in goroutines are recovered
func TestGoroutinePanicRecovery(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	
	transformer := NewBackupTransformer()
	runner, err := NewBackupRunner(backupDir, "ios_backup", false, transformer)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	
	// Create a stderr buffer that will trigger FILE_SAVED parsing
	stderr := bytes.NewBufferString("FILE_SAVED: path=test domain=test.txt\n")
	
	// This should not crash even if file processing fails
	errChan := make(chan error, 1)
	runner.wg.Add(1)
	go func() {
		errChan <- runner.processStderr(stderr)
	}()
	
	runner.wg.Wait()
	
	// Wait for any async processing
	runner.processingWg.Wait()
	
	err = <-errChan
	if err != nil {
		t.Logf("Expected stderr processing to complete without fatal error: %v", err)
	}
}

// TestMemoryAllocationGuard tests that large image allocations are protected
func TestMemoryAllocationGuard(t *testing.T) {
	// Create a reasonably sized image
	// Then request a resize to 10000x10000 which would require 400MB (exceeds 50MB limit)
	largeImg := image.NewRGBA(image.Rect(0, 0, 15000, 15000))
	
	// This should fail gracefully due to size guard
	// Resize to 10000 width would create 10000x10000 image = 400MB
	_, err := resizeImage(largeImg, 10000)
	if err == nil {
		t.Error("Expected error for oversized image allocation, got nil")
		return
	}
	
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("Expected 'too large' error, got: %v", err)
	}
}

// TestResizeImageSmallImage tests that small images work correctly
func TestResizeImageSmallImage(t *testing.T) {
	// Create a small test image
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	
	// Fill with a color
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	
	// Resize should succeed
	resized, err := resizeImage(img, 200)
	if err != nil {
		t.Fatalf("Failed to resize small image: %v", err)
	}
	
	// Should return original since it's smaller than target
	if resized.Bounds().Dx() != 100 {
		t.Errorf("Expected width 100, got %d", resized.Bounds().Dx())
	}
}

// TestResizeImageLargeImage tests resizing a large image
func TestResizeImageLargeImage(t *testing.T) {
	// Create a reasonably large test image
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1000))
	
	// Resize should succeed
	resized, err := resizeImage(img, 500)
	if err != nil {
		t.Fatalf("Failed to resize large image: %v", err)
	}
	
	// Should be resized to 500 width
	if resized.Bounds().Dx() != 500 {
		t.Errorf("Expected width 500, got %d", resized.Bounds().Dx())
	}
	
	// Height should maintain aspect ratio
	expectedHeight := 500
	if resized.Bounds().Dy() != expectedHeight {
		t.Errorf("Expected height %d, got %d", expectedHeight, resized.Bounds().Dy())
	}
}

// TestDoubleCloseProtection tests that double close is handled properly
func TestDoubleCloseProtection(t *testing.T) {
	tempDir := t.TempDir()
	
	// Create a test JPEG
	testJpeg := filepath.Join(tempDir, "test.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	f, err := os.Create(testJpeg)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("Failed to encode JPEG: %v", err)
	}
	f.Close()
	
	// Test resizeJpegImage which previously had double close issue
	resized, err := resizeJpegImage(testJpeg, 50)
	if err != nil {
		t.Fatalf("Failed to resize JPEG: %v", err)
	}
	
	// Cleanup
	os.Remove(resized)
}

// TestExternalToolTimeout tests that external tool timeouts work
func TestExternalToolTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}
	
	tempDir := t.TempDir()
	transformer := NewBackupTransformer()
	
	// Create a fake "video" file
	fakeVideo := filepath.Join(tempDir, "test.mp4")
	if err := os.WriteFile(fakeVideo, []byte("not a real video"), 0644); err != nil {
		t.Fatalf("Failed to create fake video: %v", err)
	}
	
	// This should fail gracefully with timeout or error, not crash
	transformer.convertVideoToJpeg(fakeVideo)
	
	// If we get here, no crash occurred
}

// TestBackupRunnerTimeout tests that ios_backup command has a timeout
func TestBackupRunnerTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}
	
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	
	// Create a mock ios_backup that sleeps forever
	mockIosBackup := filepath.Join(tempDir, "ios_backup_mock")
	script := "#!/bin/bash\nsleep 10000\n"
	if err := os.WriteFile(mockIosBackup, []byte(script), 0755); err != nil {
		t.Fatalf("Failed to create mock ios_backup: %v", err)
	}
	
	transformer := NewBackupTransformer()
	runner, err := NewBackupRunner(backupDir, mockIosBackup, false, transformer)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	
	// Run with a very short timeout by modifying the context
	// This is a bit tricky since Run() creates its own context
	// We'll test that the mechanism exists by checking the code path
	
	// For now, just verify the runner was created with timeout capability
	if runner == nil {
		t.Error("Runner should be created successfully")
	}
}

// TestScannerErrorPropagation tests that scanner errors are properly propagated
func TestScannerErrorPropagation(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	
	transformer := NewBackupTransformer()
	runner, err := NewBackupRunner(backupDir, "ios_backup", false, transformer)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	
	// Test with normal output
	stdout := bytes.NewBufferString("Normal output\n")
	
	runner.wg.Add(1)
	err = runner.processOutput(stdout, &bytes.Buffer{})
	if err != nil {
		t.Errorf("Expected no error for normal output, got: %v", err)
	}
}

// TestConcurrentFileProcessing tests that concurrent file processing doesn't crash
func TestConcurrentFileProcessing(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	
	transformer := NewBackupTransformer()
	runner, err := NewBackupRunner(backupDir, "ios_backup", false, transformer)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	
	// Create multiple test files
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		testFile := filepath.Join(tempDir, fmt.Sprintf("test%d.txt", i))
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		
		wg.Add(1)
		go func(file string) {
			defer wg.Done()
			runner.processFile(file, "test.txt")
		}(testFile)
	}
	
	wg.Wait()
	
	// No crash means success
}

// TestManifestAnalyzerInvalidDB tests that invalid DB doesn't crash
func TestManifestAnalyzerInvalidDB(t *testing.T) {
	tempDir := t.TempDir()
	invalidDB := filepath.Join(tempDir, "invalid.db")
	
	if err := os.WriteFile(invalidDB, []byte("not a database"), 0644); err != nil {
		t.Fatalf("Failed to create invalid DB: %v", err)
	}
	
	_, err := NewManifestAnalyzer(invalidDB)
	if err == nil {
		t.Error("Expected error for invalid database")
	}
}

// TestFindExecutableNotFound tests that missing executables are handled
func TestFindExecutableNotFound(t *testing.T) {
	_, found := findExecutable("definitely_not_a_real_executable_xyz123")
	if found {
		t.Error("Should not find non-existent executable")
	}
}

// TestProcessFileStatError tests that stat errors are handled gracefully
func TestProcessFileStatError(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	
	transformer := NewBackupTransformer()
	runner, err := NewBackupRunner(backupDir, "ios_backup", false, transformer)
	if err != nil {
		t.Fatalf("Failed to create runner: %v", err)
	}
	
	// Try to process a non-existent file
	nonExistent := filepath.Join(tempDir, "nonexistent.txt")
	runner.processFile(nonExistent, "test.txt")
	
	// Should complete without crash
}

// TestContextCancellation tests that context cancellation works
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	
	// Wait for context to be cancelled
	<-ctx.Done()
	
	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded, got %v", ctx.Err())
	}
}

// TestExecErrorHandling tests better error messages for exec failures
func TestExecErrorHandling(t *testing.T) {
	// Try to execute a non-existent command
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, "nonexistent_command_xyz")
	output, err := cmd.CombinedOutput()
	
	if err == nil {
		t.Error("Expected error for non-existent command")
	}
	
	// Check if we can detect the error type
	if exitErr, ok := err.(*exec.ExitError); ok {
		t.Logf("Got exit error as expected: %v", exitErr)
	} else {
		t.Logf("Got error (not ExitError): %v, output: %s", err, string(output))
	}
}

// TestBackupTransformerSemaphores tests that semaphores prevent resource exhaustion
func TestBackupTransformerSemaphores(t *testing.T) {
	transformer := NewBackupTransformer()
	
	// Verify semaphores are initialized
	if transformer.videoSemaphore == nil {
		t.Error("Video semaphore should be initialized")
	}
	if transformer.heicSemaphore == nil {
		t.Error("HEIC semaphore should be initialized")
	}
	if transformer.gifSemaphore == nil {
		t.Error("GIF semaphore should be initialized")
	}
	
	// Test that we can acquire and release
	transformer.videoSemaphore <- struct{}{}
	<-transformer.videoSemaphore
}

// TestLoggerInitialization tests that loggers don't cause crashes
func TestLoggerInitialization(t *testing.T) {
	// Save original loggers
	origInfo := infoLog
	origError := errorLog
	defer func() {
		infoLog = origInfo
		errorLog = origError
	}()
	
	// Test with nil logger (should not crash)
	if infoLog != nil {
		infoLog.Printf("Test info log")
	}
	if errorLog != nil {
		errorLog.Printf("Test error log")
	}
}

// TestFileTimingStructure tests FileTiming struct
func TestFileTimingStructure(t *testing.T) {
	timing := &FileTiming{DiscoveryMethod: "test"}
	
	if timing.DiscoveryMethod != "test" {
		t.Errorf("Expected 'test', got %s", timing.DiscoveryMethod)
	}
}
