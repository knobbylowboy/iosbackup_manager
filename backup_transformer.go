package main

import (
	"context"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/image/webp"
)

var (
	executableDir string
	executableDirOnce sync.Once
)

// getExecutableDir returns the directory containing the executable
// During tests, this will use the current working directory instead
func getExecutableDir() string {
	executableDirOnce.Do(func() {
		// Check if we're running in a test (test binary name contains "test")
		execPath, err := os.Executable()
		if err != nil {
			if errorLog != nil {
				errorLog.Printf("Warning: Could not determine executable path: %v", err)
			}
			executableDir = "."
			return
		}
		
		// If running as a test binary, use current working directory
		// Test binaries are typically in temp directories
		if strings.Contains(filepath.Base(execPath), "test") {
			// Use current working directory for tests
			wd, err := os.Getwd()
			if err == nil {
				executableDir = wd
				return
			}
		}
		
		executableDir = filepath.Dir(execPath)
	})
	return executableDir
}

// findExecutable looks for an executable in the project root, then PATH
func findExecutable(name string) (string, bool) {
	// First, try in the executable directory (project root)
	execDir := getExecutableDir()
	localPath := filepath.Join(execDir, name)
	if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
		return localPath, true
	}
	
	// Also try with .exe extension on Windows
	if filepath.Ext(name) == "" {
		localPathExe := localPath + ".exe"
		if info, err := os.Stat(localPathExe); err == nil && !info.IsDir() {
			return localPathExe, true
		}
	}
	
	// Also try current working directory (useful for tests and when executable is in different location)
	wd, err := os.Getwd()
	if err == nil && wd != execDir {
		wdPath := filepath.Join(wd, name)
		if info, err := os.Stat(wdPath); err == nil && !info.IsDir() {
			return wdPath, true
		}
		// Try with .exe extension
		if filepath.Ext(name) == "" {
			wdPathExe := wdPath + ".exe"
			if info, err := os.Stat(wdPathExe); err == nil && !info.IsDir() {
				return wdPathExe, true
			}
		}
	}
	
	// Fall back to PATH lookup
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}
	
	return "", false
}

// Image resize constants (matching Dart PdfConfig)
const (
	standardImageWidth  = 500 // Standard image width for PDF
	thumbnailImageWidth = 150 // Thumbnail width (not currently used, but available)
	jpegQuality         = 85  // JPEG quality (matching Dart implementation)
)

// resizeImage resizes an image to the specified width while maintaining aspect ratio
// Uses a simple nearest-neighbor algorithm - good enough for our use case
func resizeImage(img image.Image, maxWidth int) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// If image is already smaller than maxWidth, return as-is
	if width <= maxWidth {
		return img
	}

	// Calculate new height maintaining aspect ratio
	newHeight := (height * maxWidth) / width
	if newHeight < 1 {
		newHeight = 1
	}

	// Create new RGBA image for resizing
	resized := image.NewRGBA(image.Rect(0, 0, maxWidth, newHeight))
	
	// Simple nearest-neighbor resize
	for y := 0; y < newHeight; y++ {
		for x := 0; x < maxWidth; x++ {
			srcX := bounds.Min.X + (x * width) / maxWidth
			srcY := bounds.Min.Y + (y * height) / newHeight
			resized.Set(x, y, img.At(srcX, srcY))
		}
	}

	return resized
}

// BackupTransformer handles conversion of backup files
type BackupTransformer struct {
	detector *ContentDetector
	
	// Semaphores to limit concurrent operations
	videoSemaphore chan struct{}
	heicSemaphore  chan struct{}
	gifSemaphore   chan struct{}
	
	// Configuration flags
	truncateUnknown   bool // If true, truncate unknown file types to 0 bytes; if false, delete them
	mediaTransformOnly bool // If true, only transform media files and skip processing other files
}

// NewBackupTransformer creates a new backup transformer
func NewBackupTransformer(truncateUnknown bool, mediaTransformOnly bool) *BackupTransformer {
	// Create semaphores with appropriate limits
	// Video: 5 concurrent, HEIC: 100 concurrent, GIF: 5 concurrent
	videoSem := make(chan struct{}, 5)
	heicSem := make(chan struct{}, 100)
	gifSem := make(chan struct{}, 5)
	
	return &BackupTransformer{
		detector:          NewContentDetector(),
		videoSemaphore:    videoSem,
		heicSemaphore:     heicSem,
		gifSemaphore:      gifSem,
		truncateUnknown:   truncateUnknown,
		mediaTransformOnly: mediaTransformOnly,
	}
}

// ProcessFile processes a file based on its detected type
// Returns true if the file was processed/converted/deleted, false otherwise
// Files that are not our desired types (HEIC, GIF, videos) or SQLite databases are deleted permanently
func (bt *BackupTransformer) ProcessFile(filePath string) bool {
	// Handle files in Snapshot directories
	if strings.Contains(filePath, "/Snapshot/") || strings.Contains(filePath, "\\Snapshot\\") {
		if bt.mediaTransformOnly {
			return false
		}
		// Truncate files in Snapshot directories to 0 bytes once they've stabilized
		// This preserves the file structure while removing content
		infoLog.Printf("Truncating file in Snapshot directory to 0 bytes: %s", filepath.Base(filePath))
		if err := os.Truncate(filePath, 0); err != nil {
			errorLog.Printf("Error truncating Snapshot file %s: %v", filePath, err)
			return false
		}
		infoLog.Printf("Successfully truncated Snapshot file to 0 bytes: %s", filepath.Base(filePath))
		return true
	}

	// Detect file type
	fileInfo, err := bt.detector.DetectFileType(filePath)
	if err != nil {
		errorLog.Printf("Error detecting file type for %s: %v", filePath, err)
		return false
	}

	switch fileInfo.ContentType {
	case "HEIC":
		return bt.convertHeicToJpeg(filePath)
	case "GIF":
		return bt.convertGifToJpeg(filePath)
	case "JPEG":
		return bt.resizeJpeg(filePath)
	case "PNG":
		return bt.convertPngToJpeg(filePath)
	case "WEBP":
		return bt.convertWebpToJpeg(filePath)
	case "MP4", "MOV", "AVI", "MPG", "WMV", "FLV", "WebM", "MKV":
		return bt.convertVideoToJpeg(filePath)
	case "SQLite":
		// Keep SQLite databases - don't delete them
		if bt.mediaTransformOnly {
			return false
		}
		infoLog.Printf("Keeping SQLite database: %s", filepath.Base(filePath))
		return false
	case "PLIST":
		// Keep important backup PLIST files (manifest.plist, status.plist)
		// Truncate other PLIST files to 0 bytes
		if bt.mediaTransformOnly {
			return false
		}
		fileName := strings.ToLower(filepath.Base(filePath))
		if fileName == "manifest.plist" || fileName == "status.plist" {
			infoLog.Printf("Keeping important PLIST file: %s", filepath.Base(filePath))
			return false
		}
		// Truncate other PLIST files to 0 bytes
		infoLog.Printf("Truncating PLIST file to 0 bytes: %s", filepath.Base(filePath))
		if err := os.Truncate(filePath, 0); err != nil {
			errorLog.Printf("Error truncating PLIST file %s: %v", filePath, err)
			return false
		}
		infoLog.Printf("Successfully truncated PLIST file to 0 bytes: %s", filepath.Base(filePath))
		return true
	default:
		// File type not supported for conversion
		if bt.mediaTransformOnly {
			return false
		}
		if bt.truncateUnknown {
			// Truncate to 0 bytes if flag is set
			infoLog.Printf("Truncating unsupported file type (%s) to 0 bytes: %s", fileInfo.ContentType, filepath.Base(filePath))
			if err := os.Truncate(filePath, 0); err != nil {
				errorLog.Printf("Error truncating file %s: %v", filePath, err)
				return false
			}
			infoLog.Printf("Successfully truncated to 0 bytes: %s", filepath.Base(filePath))
			return true
		} else {
			// Delete the file if flag is not set
			infoLog.Printf("Deleting unsupported file type (%s): %s", fileInfo.ContentType, filepath.Base(filePath))
			if err := os.Remove(filePath); err != nil {
				errorLog.Printf("Error deleting file %s: %v", filePath, err)
				return false
			}
			infoLog.Printf("Successfully deleted: %s", filepath.Base(filePath))
			return true
		}
	}
}

// convertHeicToJpeg converts a HEIC file to JPEG, overwriting the original
// Uses ImageMagick via CGO bindings (requires ImageMagick library)
func (bt *BackupTransformer) convertHeicToJpeg(heicFilePath string) bool {
	bt.heicSemaphore <- struct{}{} // Acquire semaphore
	defer func() { <-bt.heicSemaphore }() // Release semaphore

	infoLog.Printf("Converting HEIC to JPEG: %s", filepath.Base(heicFilePath))

	// Try to find heic-converter in project root, then PATH
	heicConverter, found := findExecutable("heic-converter")
	if !found {
		infoLog.Printf("HEIC converter not found in project root or PATH, skipping conversion")
		return false
	}

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(heicFilePath), "heic_conv_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for HEIC conversion: %v", err)
		return false
	}
	tempJpegPath := tempJpeg.Name()
	tempJpeg.Close()
	defer os.Remove(tempJpegPath) // Clean up temp file on exit

	// Run conversion
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, heicConverter, heicFilePath, tempJpegPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorLog.Printf("HEIC conversion failed for %s: %v, output: %s", heicFilePath, err, string(output))
		return false
	}

	// Check if temp file was created successfully
	if _, err := os.Stat(tempJpegPath); os.IsNotExist(err) {
		errorLog.Printf("HEIC conversion failed: output file not created")
		return false
	}

	// Resize the converted JPEG image
	resizedJpegPath, err := resizeJpegImage(tempJpegPath, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing HEIC-converted JPEG: %v, using original size", err)
		// Continue with original size if resize fails
		resizedJpegPath = tempJpegPath
	} else {
		// Remove the original temp file if resize succeeded
		os.Remove(tempJpegPath)
	}

	// Replace original file with resized JPEG
	if err := os.Rename(resizedJpegPath, heicFilePath); err != nil {
		errorLog.Printf("Error replacing original HEIC file: %v", err)
		return false
	}

	infoLog.Printf("Successfully converted and resized HEIC to JPEG: %s", filepath.Base(heicFilePath))
	return true
}

// convertGifToJpeg converts a GIF file to JPEG, overwriting the original
// Uses Go's standard library for pure Go implementation
func (bt *BackupTransformer) convertGifToJpeg(gifFilePath string) bool {
	bt.gifSemaphore <- struct{}{} // Acquire semaphore
	defer func() { <-bt.gifSemaphore }() // Release semaphore

	infoLog.Printf("Converting GIF to JPEG: %s", filepath.Base(gifFilePath))

	// Open and decode GIF file
	file, err := os.Open(gifFilePath)
	if err != nil {
		errorLog.Printf("Error opening GIF file: %v", err)
		return false
	}
	defer file.Close()

	// Decode GIF
	gifImg, err := gif.Decode(file)
	if err != nil {
		errorLog.Printf("Error decoding GIF: %v", err)
		return false
	}

	// Resize GIF image before encoding as JPEG
	resizedImg := resizeImage(gifImg, standardImageWidth)

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(gifFilePath), "gif_conv_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for GIF conversion: %v", err)
		return false
	}
	tempJpegPath := tempJpeg.Name()
	defer tempJpeg.Close()
	defer os.Remove(tempJpegPath) // Clean up temp file on exit

	// Encode resized image as JPEG with quality 85 (matching Dart implementation)
	if err := jpeg.Encode(tempJpeg, resizedImg, &jpeg.Options{Quality: jpegQuality}); err != nil {
		errorLog.Printf("Error encoding JPEG: %v", err)
		return false
	}
	tempJpeg.Close()

	// Replace original file with converted JPEG
	if err := os.Rename(tempJpegPath, gifFilePath); err != nil {
		errorLog.Printf("Error replacing original GIF file: %v", err)
		return false
	}

	infoLog.Printf("Successfully converted and resized GIF to JPEG: %s", filepath.Base(gifFilePath))
	return true
}

// resizeJpeg resizes a JPEG file to the standard width, overwriting the original
func (bt *BackupTransformer) resizeJpeg(jpegFilePath string) bool {
	infoLog.Printf("Resizing JPEG: %s", filepath.Base(jpegFilePath))

	// Resize the JPEG image
	resizedJpegPath, err := resizeJpegImage(jpegFilePath, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing JPEG: %v, keeping original size", err)
		// Keep original if resize fails
		return false
	}

	// Replace original file with resized JPEG
	if err := os.Rename(resizedJpegPath, jpegFilePath); err != nil {
		errorLog.Printf("Error replacing original JPEG file: %v", err)
		os.Remove(resizedJpegPath)
		return false
	}

	infoLog.Printf("Successfully resized JPEG: %s", filepath.Base(jpegFilePath))
	return true
}

// convertPngToJpeg converts a PNG file to JPEG and resizes it, overwriting the original
func (bt *BackupTransformer) convertPngToJpeg(pngFilePath string) bool {
	infoLog.Printf("Converting PNG to JPEG: %s", filepath.Base(pngFilePath))

	// Open and decode PNG file
	file, err := os.Open(pngFilePath)
	if err != nil {
		errorLog.Printf("Error opening PNG file: %v", err)
		return false
	}
	defer file.Close()

	// Decode PNG
	pngImg, err := png.Decode(file)
	if err != nil {
		errorLog.Printf("Error decoding PNG: %v", err)
		return false
	}

	// Resize PNG image before encoding as JPEG
	resizedImg := resizeImage(pngImg, standardImageWidth)

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(pngFilePath), "png_conv_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for PNG conversion: %v", err)
		return false
	}
	tempJpegPath := tempJpeg.Name()
	defer tempJpeg.Close()
	defer os.Remove(tempJpegPath) // Clean up temp file on exit

	// Encode resized image as JPEG with quality 85 (matching Dart implementation)
	if err := jpeg.Encode(tempJpeg, resizedImg, &jpeg.Options{Quality: jpegQuality}); err != nil {
		errorLog.Printf("Error encoding JPEG: %v", err)
		return false
	}
	tempJpeg.Close()

	// Replace original file with converted JPEG
	if err := os.Rename(tempJpegPath, pngFilePath); err != nil {
		errorLog.Printf("Error replacing original PNG file: %v", err)
		return false
	}

	infoLog.Printf("Successfully converted and resized PNG to JPEG: %s", filepath.Base(pngFilePath))
	return true
}

// convertWebpToJpeg converts a WEBP file to JPEG and resizes it, overwriting the original
func (bt *BackupTransformer) convertWebpToJpeg(webpFilePath string) bool {
	infoLog.Printf("Converting WEBP to JPEG: %s", filepath.Base(webpFilePath))

	// Open and decode WEBP file
	file, err := os.Open(webpFilePath)
	if err != nil {
		errorLog.Printf("Error opening WEBP file: %v", err)
		return false
	}
	defer file.Close()

	// Decode WEBP
	webpImg, err := webp.Decode(file)
	if err != nil {
		errorLog.Printf("Error decoding WEBP: %v", err)
		return false
	}

	// Resize WEBP image before encoding as JPEG
	resizedImg := resizeImage(webpImg, standardImageWidth)

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(webpFilePath), "webp_conv_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for WEBP conversion: %v", err)
		return false
	}
	tempJpegPath := tempJpeg.Name()
	defer tempJpeg.Close()
	defer os.Remove(tempJpegPath) // Clean up temp file on exit

	// Encode resized image as JPEG with quality 85 (matching Dart implementation)
	if err := jpeg.Encode(tempJpeg, resizedImg, &jpeg.Options{Quality: jpegQuality}); err != nil {
		errorLog.Printf("Error encoding JPEG: %v", err)
		return false
	}
	tempJpeg.Close()

	// Replace original file with converted JPEG
	if err := os.Rename(tempJpegPath, webpFilePath); err != nil {
		errorLog.Printf("Error replacing original WEBP file: %v", err)
		return false
	}

	infoLog.Printf("Successfully converted and resized WEBP to JPEG: %s", filepath.Base(webpFilePath))
	return true
}

// convertVideoToJpeg generates a JPEG thumbnail from a video, overwriting the original
// Uses ffmpeg via exec (requires ffmpeg to be available)
func (bt *BackupTransformer) convertVideoToJpeg(videoFilePath string) bool {
	bt.videoSemaphore <- struct{}{} // Acquire semaphore
	defer func() { <-bt.videoSemaphore }() // Release semaphore

	infoLog.Printf("Converting video to JPEG thumbnail: %s", filepath.Base(videoFilePath))

	// Determine seek position (similar to Dart implementation)
	seekSeconds := bt.determineThumbnailSeekSeconds(videoFilePath)
	seekTimestamp := formatSeekTimestamp(seekSeconds)

	// Try to find ffmpeg in project root, then PATH
	ffmpegPath, found := findExecutable("ffmpeg")
	if !found {
		infoLog.Printf("ffmpeg not found in project root or PATH, skipping video conversion")
		return false
	}

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(videoFilePath), "video_thumb_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for video conversion: %v", err)
		return false
	}
	tempJpegPath := tempJpeg.Name()
	tempJpeg.Close()
	defer os.Remove(tempJpegPath) // Clean up temp file on exit

	// Run ffmpeg to extract thumbnail
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	args := []string{
		"-ss", seekTimestamp,
		"-i", videoFilePath,
		"-vframes", "1",
		"-f", "image2",
		"-update", "1",
		"-y",
		tempJpegPath,
	}

	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorLog.Printf("Video thumbnail generation failed for %s: %v, output: %s", videoFilePath, err, string(output))
		return false
	}

	// Check if temp file was created successfully
	if _, err := os.Stat(tempJpegPath); os.IsNotExist(err) {
		errorLog.Printf("Video conversion failed: output file not created")
		return false
	}

	// Resize the video thumbnail
	resizedJpegPath, err := resizeJpegImage(tempJpegPath, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing video thumbnail: %v, using original size", err)
		// Continue with original size if resize fails
		resizedJpegPath = tempJpegPath
	} else {
		// Remove the original temp file if resize succeeded
		os.Remove(tempJpegPath)
	}

	// Replace original file with resized JPEG thumbnail
	if err := os.Rename(resizedJpegPath, videoFilePath); err != nil {
		errorLog.Printf("Error replacing original video file: %v", err)
		return false
	}

	infoLog.Printf("Successfully converted and resized video to JPEG thumbnail: %s", filepath.Base(videoFilePath))
	return true
}

const (
	maxThumbnailSeekSeconds      = 0.5
	fallbackThumbnailSeekSeconds = 0.1
)

// determineThumbnailSeekSeconds determines the seek position for video thumbnail extraction
func (bt *BackupTransformer) determineThumbnailSeekSeconds(videoFilePath string) float64 {
	duration := bt.probeVideoDuration(videoFilePath)
	if duration == nil {
		infoLog.Printf("Video duration unavailable, defaulting to first frame for thumbnail")
		return fallbackThumbnailSeekSeconds
	}

	safeSeek := *duration / 2
	if safeSeek > maxThumbnailSeekSeconds {
		safeSeek = maxThumbnailSeekSeconds
	}

	if safeSeek <= 0 {
		infoLog.Printf("Video duration too short, using first frame for thumbnail")
		return fallbackThumbnailSeekSeconds
	}

	return safeSeek
}

// probeVideoDuration probes the video file to get its duration
// Uses ffprobe via exec (requires ffprobe to be available)
func (bt *BackupTransformer) probeVideoDuration(videoFilePath string) *float64 {
	// Try to find ffprobe in project root, then PATH
	ffprobePath, found := findExecutable("ffprobe")
	if !found {
		infoLog.Printf("ffprobe not found in project root or PATH, cannot determine video duration")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	args := []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoFilePath,
	}

	cmd := exec.CommandContext(ctx, ffprobePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorLog.Printf("ffprobe duration lookup failed: %v", err)
		return nil
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" || outputStr == "N/A" {
		return nil
	}

	var duration float64
	if _, err := fmt.Sscanf(outputStr, "%f", &duration); err != nil {
		return nil
	}

	if duration <= 0 {
		return nil
	}

	return &duration
}

// formatSeekTimestamp formats seconds into a timestamp string for ffmpeg
func formatSeekTimestamp(seconds float64) string {
	if seconds <= 0 {
		return "0"
	}

	// Format to 3 decimal places, then trim trailing zeros
	formatted := fmt.Sprintf("%.3f", seconds)
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")

	if formatted == "" {
		return "0"
	}

	return formatted
}

// resizeJpegImage reads a JPEG file, resizes it, and writes a new resized JPEG file
func resizeJpegImage(jpegPath string, maxWidth int) (string, error) {
	// Open and decode JPEG
	file, err := os.Open(jpegPath)
	if err != nil {
		return "", fmt.Errorf("failed to open JPEG: %v", err)
	}
	defer file.Close()

	jpegImg, err := jpeg.Decode(file)
	if err != nil {
		return "", fmt.Errorf("failed to decode JPEG: %v", err)
	}

	// Resize the image
	resizedImg := resizeImage(jpegImg, maxWidth)

	// Create temporary output file for resized JPEG
	tempResized, err := os.CreateTemp(filepath.Dir(jpegPath), "resized_*.jpg")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	resizedPath := tempResized.Name()
	tempResized.Close()

	// Write resized JPEG
	resizedFile, err := os.Create(resizedPath)
	if err != nil {
		os.Remove(resizedPath)
		return "", fmt.Errorf("failed to create resized file: %v", err)
	}
	defer resizedFile.Close()

	if err := jpeg.Encode(resizedFile, resizedImg, &jpeg.Options{Quality: jpegQuality}); err != nil {
		os.Remove(resizedPath)
		return "", fmt.Errorf("failed to encode resized JPEG: %v", err)
	}

	return resizedPath, nil
}

// BackupFileMonitor monitors a directory for backup files and processes them
type BackupFileMonitor struct {
	watchDir       string
	transformer    *BackupTransformer
	watcher        *fsnotify.Watcher
	processedFiles map[string]time.Time
	scannedDirs    map[string]time.Time // Track when we last scanned each directory
	mu             sync.Mutex
	stopChan       chan struct{}
	wg             sync.WaitGroup
}

// NewBackupFileMonitor creates a new backup file monitor
func NewBackupFileMonitor(watchDir string, transformer *BackupTransformer) (*BackupFileMonitor, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %v", err)
	}

	return &BackupFileMonitor{
		watchDir:       watchDir,
		transformer:    transformer,
		watcher:        watcher,
		processedFiles: make(map[string]time.Time),
		scannedDirs:    make(map[string]time.Time),
		stopChan:       make(chan struct{}),
	}, nil
}

// Start begins monitoring the directory
func (bfm *BackupFileMonitor) Start() error {
	// Add the watch directory
	if err := bfm.watcher.Add(bfm.watchDir); err != nil {
		return fmt.Errorf("failed to add watch directory: %v", err)
	}

	// Watch subdirectories and process existing files
	if err := filepath.Walk(bfm.watchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return bfm.watcher.Add(path)
		}
		// Process existing files asynchronously
		if !info.IsDir() {
			go func(filePath string) {
				bfm.waitForFileStable(filePath)
				bfm.processFile(filePath)
			}(path)
		}
		return nil
	}); err != nil {
		errorLog.Printf("Warning: failed to add some subdirectories to watch: %v", err)
	}

	// Start the event processing goroutine
	bfm.wg.Add(1)
	go bfm.processEvents()

	// Start periodic scanning goroutine to catch files that fsnotify might miss
	bfm.wg.Add(1)
	go bfm.periodicScan()

	return nil
}

// processEvents handles file system events
func (bfm *BackupFileMonitor) processEvents() {
	defer bfm.wg.Done()

	for {
		select {
		case <-bfm.stopChan:
			return
		case event, ok := <-bfm.watcher.Events:
			if !ok {
				return
			}
			bfm.handleEvent(event)
		case err, ok := <-bfm.watcher.Errors:
			if !ok {
				return
			}
			errorLog.Printf("File watcher error: %v", err)
		}
	}
}

// handleEvent processes individual file system events
func (bfm *BackupFileMonitor) handleEvent(event fsnotify.Event) {
	// Only process CREATE and WRITE events
	if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) {
		return
	}

	// Skip directories
	if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
		// If it's a new directory, add it to the watcher
		if event.Has(fsnotify.Create) {
			bfm.watcher.Add(event.Name)
		}
		return
	}

	// Check if we've already processed this file recently
	bfm.mu.Lock()
	lastProcessed, exists := bfm.processedFiles[event.Name]
	now := time.Now()

	// Only process if file hasn't been processed in the last 2 seconds
	if exists && now.Sub(lastProcessed) < 2*time.Second {
		bfm.mu.Unlock()
		return
	}

	bfm.processedFiles[event.Name] = now
	bfm.mu.Unlock()

	// Process the file after ensuring it's stable (not being written to)
	go func(filename string) {
		bfm.waitForFileStable(filename)
		bfm.processFile(filename)
	}(event.Name)
}

// waitForFileStable waits for a file to stabilize (stop changing size) before processing
// This prevents reading files that are still being written by the backup process
func (bfm *BackupFileMonitor) waitForFileStable(filePath string) {
	const (
		checkInterval = 200 * time.Millisecond // Check every 200ms
		stableTime    = 500 * time.Millisecond // File must be stable for 500ms
		maxWaitTime   = 30 * time.Second        // Maximum wait time
	)

	startTime := time.Now()
	var lastSize int64 = -1
	stableSince := time.Now()

	for {
		// Check if we've exceeded max wait time
		if time.Since(startTime) > maxWaitTime {
			infoLog.Printf("File %s did not stabilize within %v, proceeding anyway", filepath.Base(filePath), maxWaitTime)
			return
		}

		// Check if file exists
		stat, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			// File was deleted, don't process
			return
		}
		if err != nil {
			// Other error, wait a bit and retry
			time.Sleep(checkInterval)
			continue
		}

		currentSize := stat.Size()

		// If size changed, reset stability timer
		if currentSize != lastSize {
			lastSize = currentSize
			stableSince = time.Now()
			time.Sleep(checkInterval)
			continue
		}

		// If size hasn't changed, check if it's been stable long enough
		// For first check (lastSize == -1), we need at least one stable check
		if lastSize != -1 && time.Since(stableSince) >= stableTime {
			// File is stable, proceed
			return
		}

		// Size is same but not stable long enough yet (or first iteration)
		time.Sleep(checkInterval)
	}
}

// processFile processes a file if it's a supported backup file type
func (bfm *BackupFileMonitor) processFile(filePath string) {
	// Skip if file no longer exists (might have been temporary)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return
	}

	// Process the file
	bfm.transformer.ProcessFile(filePath)
}

// periodicScan periodically scans the directory for new files that might have been missed by fsnotify
// Uses directory modification times to avoid scanning unchanged directories
func (bfm *BackupFileMonitor) periodicScan() {
	defer bfm.wg.Done()
	
	const scanInterval = 30 * time.Second // Scan every 30 seconds (less frequent to reduce cost)
	const dirScanCooldown = 60 * time.Second // Don't rescan a directory for 60 seconds after scanning it
	
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bfm.stopChan:
			return
		case <-ticker.C:
			bfm.scanForNewFiles(dirScanCooldown)
		}
	}
}

// scanForNewFiles walks the directory tree and processes any files that haven't been processed yet
// Only scans directories that have been modified recently or haven't been scanned recently
func (bfm *BackupFileMonitor) scanForNewFiles(dirScanCooldown time.Duration) {
	now := time.Now()
	
	filepath.Walk(bfm.watchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on error
		}
		
		if info.IsDir() {
			// Check if we should scan this directory
			bfm.mu.Lock()
			lastScanned, scanned := bfm.scannedDirs[path]
			shouldScan := !scanned || now.Sub(lastScanned) >= dirScanCooldown
			
			// Also check if directory was modified recently (within last 2 minutes)
			dirModTime := info.ModTime()
			recentlyModified := now.Sub(dirModTime) < 2*time.Minute
			
			if shouldScan && recentlyModified {
				bfm.scannedDirs[path] = now
			}
			bfm.mu.Unlock()
			
			// Skip scanning this directory if it hasn't been modified recently and we scanned it recently
			if !shouldScan || (!recentlyModified && scanned) {
				return filepath.SkipDir
			}
			
			return nil
		}

		// Process files in directories we're scanning
		bfm.mu.Lock()
		lastProcessed, exists := bfm.processedFiles[path]
		shouldProcess := !exists || now.Sub(lastProcessed) >= 2*time.Second
		bfm.mu.Unlock()

		if shouldProcess {
			// Process the file asynchronously
			go func(filePath string) {
				bfm.waitForFileStable(filePath)
				bfm.processFile(filePath)
			}(path)
		}

		return nil
	})
}

// Stop stops the monitor gracefully
func (bfm *BackupFileMonitor) Stop() {
	close(bfm.stopChan)
	bfm.watcher.Close()
	bfm.wg.Wait()
	infoLog.Println("Backup file monitor stopped")
}

