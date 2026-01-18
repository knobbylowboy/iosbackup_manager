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

	"golang.org/x/image/webp"
)

var (
	executableDir     string
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

// findExecutable looks for an executable in libraries folder, project root, then PATH
func findExecutable(name string) (string, bool) {
	execDir := getExecutableDir()

	// Priority 1: Try in libraries subdirectory
	librariesPath := filepath.Join(execDir, "libraries", name)
	if info, err := os.Stat(librariesPath); err == nil && !info.IsDir() {
		return librariesPath, true
	}

	// Also try with .exe extension on Windows
	if filepath.Ext(name) == "" {
		librariesPathExe := librariesPath + ".exe"
		if info, err := os.Stat(librariesPathExe); err == nil && !info.IsDir() {
			return librariesPathExe, true
		}
	}

	// Priority 2: Try in the executable directory (project root)
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

	// Priority 3: Try current working directory (useful for tests)
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

	// Priority 4: Fall back to PATH lookup
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	return "", false
}

// Image resize constants (matching Dart PdfConfig)
const (
	standardImageWidth = 500 // Standard image width for PDF
	jpegQuality        = 85  // JPEG quality (matching Dart implementation)
)

// resizeImage resizes an image to the specified width while maintaining aspect ratio
// Uses a simple nearest-neighbor algorithm - good enough for our use case
// Includes memory allocation guards to prevent OOM crashes
func resizeImage(img image.Image, maxWidth int) (image.Image, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// If image is already smaller than maxWidth, return as-is
	if width <= maxWidth {
		return img, nil
	}

	// Calculate new height maintaining aspect ratio
	newHeight := (height * maxWidth) / width
	if newHeight < 1 {
		newHeight = 1
	}

	// Guard against unreasonably large allocations (> 50MB for a single image)
	// RGBA uses 4 bytes per pixel
	estimatedBytes := int64(maxWidth) * int64(newHeight) * 4
	const maxAllocationBytes = 50 * 1024 * 1024 // 50 MB
	if estimatedBytes > maxAllocationBytes {
		return nil, fmt.Errorf("image too large to resize safely: would require %d MB", estimatedBytes/(1024*1024))
	}

	// Create new RGBA image for resizing (with panic recovery in case of allocation failure)
	var resized *image.RGBA
	var allocationErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				if errorLog != nil {
					errorLog.Printf("Memory allocation panic during image resize: %v", r)
				}
				allocationErr = fmt.Errorf("memory allocation failed: %v", r)
			}
		}()
		resized = image.NewRGBA(image.Rect(0, 0, maxWidth, newHeight))
	}()

	if allocationErr != nil {
		return nil, allocationErr
	}

	if resized == nil {
		return nil, fmt.Errorf("failed to allocate memory for resized image")
	}

	// Simple nearest-neighbor resize
	for y := 0; y < newHeight; y++ {
		for x := 0; x < maxWidth; x++ {
			srcX := bounds.Min.X + (x*width)/maxWidth
			srcY := bounds.Min.Y + (y*height)/newHeight
			resized.Set(x, y, img.At(srcX, srcY))
		}
	}

	return resized, nil
}

// FileTiming holds timing information for file processing
type FileTiming struct {
	CreatedTime             time.Time // Time the file was created (ModTime)
	DiscoveredTime          time.Time // Time the file was discovered
	TransformationStartTime time.Time // Time transformation started
	DiscoveryMethod         string    // How the file was discovered: "notify" or "scan"
}

// BackupTransformer handles conversion of backup files
type BackupTransformer struct {
	// Semaphores to limit concurrent operations
	videoSemaphore chan struct{}
	heicSemaphore  chan struct{}
	gifSemaphore   chan struct{}

	// Queue depth tracking (set by BackupRunner)
	queueDepth     func() (active int64, total int64) // Function to get current queue depth
	incrementTotal func()                             // Function to increment total count when transformation starts
}

// NewBackupTransformer creates a new backup transformer
func NewBackupTransformer() *BackupTransformer {
	// Create semaphores with appropriate limits
	// Video: 5 concurrent, HEIC: 100 concurrent, GIF: 5 concurrent
	videoSem := make(chan struct{}, 5)
	heicSem := make(chan struct{}, 100)
	gifSem := make(chan struct{}, 5)

	return &BackupTransformer{
		videoSemaphore: videoSem,
		heicSemaphore:  heicSem,
		gifSemaphore:   gifSem,
	}
}

// getQueueDepthString returns a formatted queue depth string like "(2 of 99)"
func (bt *BackupTransformer) getQueueDepthString() string {
	if bt.queueDepth == nil {
		return ""
	}
	active, total := bt.queueDepth()
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("(%d of %d) ", active, total)
}

// ProcessFileByExtension processes a file based on its file extension from ios_backup domain
// This is faster and more reliable than content detection since ios_backup provides the original filename
func (bt *BackupTransformer) ProcessFileByExtension(filePath string, fileExt string, timing *FileTiming) {
	// Set transformation start time
	if timing != nil {
		timing.TransformationStartTime = time.Now()
	}

	// Process based on file extension (case-insensitive)
	switch fileExt {
	case ".heic":
		bt.convertHeicToJpeg(filePath)
	case ".gif":
		bt.convertGifToJpeg(filePath)
	case ".jpg", ".jpeg":
		bt.resizeJpeg(filePath)
	case ".png":
		bt.convertPngToJpeg(filePath)
	case ".webp":
		bt.convertWebpToJpeg(filePath)
	case ".mp4", ".mov", ".avi", ".mpg", ".mpeg", ".wmv", ".flv", ".webm", ".mkv", ".m4v",
		".3gp", ".3gpp", ".ts", ".m2ts", ".mts", ".vob", ".asf", ".ogv", ".ogg", ".f4v":
		bt.convertVideoToJpeg(filePath)
	default:
		// Not a media file, skip (ios_backup already filtered what we need)
	}
}

// convertHeicToJpeg converts a HEIC file to JPEG, overwriting the original
// Uses heic-converter external tool
func (bt *BackupTransformer) convertHeicToJpeg(heicFilePath string) {
	bt.heicSemaphore <- struct{}{}        // Acquire semaphore
	defer func() { <-bt.heicSemaphore }() // Release semaphore

	// Increment total count when transformation actually starts
	if bt.incrementTotal != nil {
		bt.incrementTotal()
	}

	infoLog.Printf("%sConverting HEIC to JPEG: %s", bt.getQueueDepthString(), filepath.Base(heicFilePath))

	// Record transformation start time for duration calculation
	transformStart := time.Now()

	// Try to find heic-converter in project root, then PATH
	heicConverter, found := findExecutable("heic-converter")
	if !found {
		infoLog.Printf("HEIC converter not found in project root or PATH, skipping conversion for %s", filepath.Base(heicFilePath))
		return
	}

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(heicFilePath), "heic_conv_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for HEIC conversion: %v", err)
		return
	}
	tempJpegPath := tempJpeg.Name()
	if err := tempJpeg.Close(); err != nil {
		errorLog.Printf("Warning: error closing temp file: %v", err)
	}

	// Setup cleanup
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			if err := os.Remove(tempJpegPath); err != nil && !os.IsNotExist(err) {
				errorLog.Printf("Warning: failed to remove temp file %s: %v", tempJpegPath, err)
			}
		}
	}()

	// Run conversion with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, heicConverter, heicFilePath, tempJpegPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Provide better error context
		if ctx.Err() == context.DeadlineExceeded {
			errorLog.Printf("HEIC conversion timed out after 30 seconds for %s", heicFilePath)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			errorLog.Printf("HEIC converter crashed or failed for %s: exit code %d, output: %s",
				heicFilePath, exitErr.ExitCode(), string(output))
		} else {
			errorLog.Printf("HEIC conversion failed for %s: %v, output: %s", heicFilePath, err, string(output))
		}
		return
	}

	// Check if temp file was created successfully
	if _, err := os.Stat(tempJpegPath); os.IsNotExist(err) {
		errorLog.Printf("HEIC conversion failed: output file not created")
		return
	}

	// Resize the converted JPEG image
	resizedJpegPath, err := resizeJpegImage(tempJpegPath, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing HEIC-converted JPEG: %v, using original size", err)
		// Continue with original size if resize fails
		resizedJpegPath = tempJpegPath
	} else {
		// Remove the original temp file if resize succeeded
		if err := os.Remove(tempJpegPath); err != nil && !os.IsNotExist(err) {
			errorLog.Printf("Warning: failed to remove intermediate temp file: %v", err)
		}
	}

	// Replace original file with resized JPEG
	if err := os.Rename(resizedJpegPath, heicFilePath); err != nil {
		errorLog.Printf("Error replacing original HEIC file: %v", err)
		return
	}

	// Don't cleanup temp file since we successfully renamed it
	cleanupTemp = false

	duration := time.Since(transformStart)
	infoLog.Printf("%sSuccessfully converted and resized HEIC to JPEG: %s [duration: %v]", bt.getQueueDepthString(), filepath.Base(heicFilePath), duration)
}

// convertGifToJpeg converts a GIF file to JPEG, overwriting the original
// Uses Go's standard library for pure Go implementation
func (bt *BackupTransformer) convertGifToJpeg(gifFilePath string) {
	bt.gifSemaphore <- struct{}{}        // Acquire semaphore
	defer func() { <-bt.gifSemaphore }() // Release semaphore

	// Increment total count when transformation actually starts
	if bt.incrementTotal != nil {
		bt.incrementTotal()
	}

	infoLog.Printf("%sConverting GIF to JPEG: %s", bt.getQueueDepthString(), filepath.Base(gifFilePath))

	// Record transformation start time for duration calculation
	transformStart := time.Now()

	// Open and decode GIF file
	file, err := os.Open(gifFilePath)
	if err != nil {
		errorLog.Printf("Error opening GIF file: %v", err)
		return
	}
	defer file.Close()

	// Decode GIF
	gifImg, err := gif.Decode(file)
	if err != nil {
		errorLog.Printf("Error decoding GIF: %v", err)
		return
	}

	// Resize GIF image before encoding as JPEG
	resizedImg, err := resizeImage(gifImg, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing GIF image: %v", err)
		return
	}

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(gifFilePath), "gif_conv_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for GIF conversion: %v", err)
		return
	}
	tempJpegPath := tempJpeg.Name()

	// Setup cleanup
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			tempJpeg.Close()
			if err := os.Remove(tempJpegPath); err != nil && !os.IsNotExist(err) {
				errorLog.Printf("Warning: failed to remove temp file %s: %v", tempJpegPath, err)
			}
		}
	}()

	// Encode resized image as JPEG with quality 85 (matching Dart implementation)
	if err := jpeg.Encode(tempJpeg, resizedImg, &jpeg.Options{Quality: jpegQuality}); err != nil {
		errorLog.Printf("Error encoding JPEG: %v", err)
		return
	}

	// Close the file before rename
	if err := tempJpeg.Close(); err != nil {
		errorLog.Printf("Warning: error closing temp JPEG file: %v", err)
	}

	// Replace original file with converted JPEG
	if err := os.Rename(tempJpegPath, gifFilePath); err != nil {
		errorLog.Printf("Error replacing original GIF file: %v", err)
		return
	}

	// Don't cleanup temp file since we successfully renamed it
	cleanupTemp = false

	duration := time.Since(transformStart)
	infoLog.Printf("%sSuccessfully converted and resized GIF to JPEG: %s [duration: %v]", bt.getQueueDepthString(), filepath.Base(gifFilePath), duration)
}

// resizeJpeg resizes a JPEG file to the standard width, overwriting the original
func (bt *BackupTransformer) resizeJpeg(jpegFilePath string) {
	// Increment total count when transformation actually starts
	if bt.incrementTotal != nil {
		bt.incrementTotal()
	}

	infoLog.Printf("%sResizing JPEG: %s", bt.getQueueDepthString(), filepath.Base(jpegFilePath))

	// Record transformation start time for duration calculation
	transformStart := time.Now()

	// Resize the JPEG image
	resizedJpegPath, err := resizeJpegImage(jpegFilePath, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing JPEG: %v, keeping original size", err)
		return
	}

	// Replace original file with resized JPEG
	if err := os.Rename(resizedJpegPath, jpegFilePath); err != nil {
		errorLog.Printf("Error replacing original JPEG file: %v", err)
		if rmErr := os.Remove(resizedJpegPath); rmErr != nil && !os.IsNotExist(rmErr) {
			errorLog.Printf("Warning: failed to cleanup resized file: %v", rmErr)
		}
		return
	}

	duration := time.Since(transformStart)
	infoLog.Printf("%sSuccessfully resized JPEG: %s [duration: %v]", bt.getQueueDepthString(), filepath.Base(jpegFilePath), duration)
}

// convertPngToJpeg converts a PNG file to JPEG and resizes it, overwriting the original
func (bt *BackupTransformer) convertPngToJpeg(pngFilePath string) {
	// Increment total count when transformation actually starts
	if bt.incrementTotal != nil {
		bt.incrementTotal()
	}

	infoLog.Printf("%sConverting PNG to JPEG: %s", bt.getQueueDepthString(), filepath.Base(pngFilePath))

	// Record transformation start time for duration calculation
	transformStart := time.Now()

	// Open and decode PNG file
	file, err := os.Open(pngFilePath)
	if err != nil {
		errorLog.Printf("Error opening PNG file: %v", err)
		return
	}
	defer file.Close()

	// Decode PNG
	pngImg, err := png.Decode(file)
	if err != nil {
		errorLog.Printf("Error decoding PNG: %v", err)
		return
	}

	// Resize PNG image before encoding as JPEG
	resizedImg, err := resizeImage(pngImg, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing PNG image: %v", err)
		return
	}

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(pngFilePath), "png_conv_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for PNG conversion: %v", err)
		return
	}
	tempJpegPath := tempJpeg.Name()

	// Setup cleanup
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			tempJpeg.Close()
			if err := os.Remove(tempJpegPath); err != nil && !os.IsNotExist(err) {
				errorLog.Printf("Warning: failed to remove temp file %s: %v", tempJpegPath, err)
			}
		}
	}()

	// Encode resized image as JPEG with quality 85 (matching Dart implementation)
	if err := jpeg.Encode(tempJpeg, resizedImg, &jpeg.Options{Quality: jpegQuality}); err != nil {
		errorLog.Printf("Error encoding JPEG: %v", err)
		return
	}

	// Close the file before rename
	if err := tempJpeg.Close(); err != nil {
		errorLog.Printf("Warning: error closing temp JPEG file: %v", err)
	}

	// Replace original file with converted JPEG
	if err := os.Rename(tempJpegPath, pngFilePath); err != nil {
		errorLog.Printf("Error replacing original PNG file: %v", err)
		return
	}

	// Don't cleanup temp file since we successfully renamed it
	cleanupTemp = false

	duration := time.Since(transformStart)
	infoLog.Printf("%sSuccessfully converted and resized PNG to JPEG: %s [duration: %v]", bt.getQueueDepthString(), filepath.Base(pngFilePath), duration)
}

// convertWebpToJpeg converts a WEBP file to JPEG and resizes it, overwriting the original
func (bt *BackupTransformer) convertWebpToJpeg(webpFilePath string) {
	// Increment total count when transformation actually starts
	if bt.incrementTotal != nil {
		bt.incrementTotal()
	}

	infoLog.Printf("%sConverting WEBP to JPEG: %s", bt.getQueueDepthString(), filepath.Base(webpFilePath))

	// Record transformation start time for duration calculation
	transformStart := time.Now()

	// Open and decode WEBP file
	file, err := os.Open(webpFilePath)
	if err != nil {
		errorLog.Printf("Error opening WEBP file: %v", err)
		return
	}
	defer file.Close()

	// Decode WEBP
	webpImg, err := webp.Decode(file)
	if err != nil {
		errorLog.Printf("Error decoding WEBP: %v", err)
		return
	}

	// Resize WEBP image before encoding as JPEG
	resizedImg, err := resizeImage(webpImg, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing WEBP image: %v", err)
		return
	}

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(webpFilePath), "webp_conv_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for WEBP conversion: %v", err)
		return
	}
	tempJpegPath := tempJpeg.Name()

	// Setup cleanup
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			tempJpeg.Close()
			if err := os.Remove(tempJpegPath); err != nil && !os.IsNotExist(err) {
				errorLog.Printf("Warning: failed to remove temp file %s: %v", tempJpegPath, err)
			}
		}
	}()

	// Encode resized image as JPEG with quality 85 (matching Dart implementation)
	if err := jpeg.Encode(tempJpeg, resizedImg, &jpeg.Options{Quality: jpegQuality}); err != nil {
		errorLog.Printf("Error encoding JPEG: %v", err)
		return
	}

	// Close the file before rename
	if err := tempJpeg.Close(); err != nil {
		errorLog.Printf("Warning: error closing temp JPEG file: %v", err)
	}

	// Replace original file with converted JPEG
	if err := os.Rename(tempJpegPath, webpFilePath); err != nil {
		errorLog.Printf("Error replacing original WEBP file: %v", err)
		return
	}

	// Don't cleanup temp file since we successfully renamed it
	cleanupTemp = false

	duration := time.Since(transformStart)
	infoLog.Printf("%sSuccessfully converted and resized WEBP to JPEG: %s [duration: %v]", bt.getQueueDepthString(), filepath.Base(webpFilePath), duration)
}

// convertVideoToJpeg generates a JPEG thumbnail from a video, overwriting the original
// Uses ffmpeg via exec (requires ffmpeg to be available)
func (bt *BackupTransformer) convertVideoToJpeg(videoFilePath string) {
	bt.videoSemaphore <- struct{}{}        // Acquire semaphore
	defer func() { <-bt.videoSemaphore }() // Release semaphore

	// Increment total count when transformation actually starts
	if bt.incrementTotal != nil {
		bt.incrementTotal()
	}

	infoLog.Printf("%sConverting video to JPEG thumbnail: %s", bt.getQueueDepthString(), filepath.Base(videoFilePath))

	// Record transformation start time for duration calculation
	transformStart := time.Now()

	// Check if the file has a video stream before attempting thumbnail generation
	if !bt.hasVideoStream(videoFilePath) {
		infoLog.Printf("%sSkipping video thumbnail generation - file has no video stream (audio-only): %s", bt.getQueueDepthString(), filepath.Base(videoFilePath))
		return
	}

	// Determine seek position (similar to Dart implementation)
	seekSeconds := bt.determineThumbnailSeekSeconds(videoFilePath)
	seekTimestamp := formatSeekTimestamp(seekSeconds)

	// Try to find ffmpeg in project root, then PATH
	ffmpegPath, found := findExecutable("ffmpeg")
	if !found {
		infoLog.Printf("ffmpeg not found in project root or PATH, skipping video conversion for %s", filepath.Base(videoFilePath))
		return
	}

	// Create temporary output file
	tempJpeg, err := os.CreateTemp(filepath.Dir(videoFilePath), "video_thumb_*.jpg")
	if err != nil {
		errorLog.Printf("Error creating temp file for video conversion: %v", err)
		return
	}
	tempJpegPath := tempJpeg.Name()
	if err := tempJpeg.Close(); err != nil {
		errorLog.Printf("Warning: error closing temp file: %v", err)
	}

	// Setup cleanup
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			if err := os.Remove(tempJpegPath); err != nil && !os.IsNotExist(err) {
				errorLog.Printf("Warning: failed to remove temp file %s: %v", tempJpegPath, err)
			}
		}
	}()

	// Run ffmpeg to extract thumbnail with timeout
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
		// Provide better error context
		if ctx.Err() == context.DeadlineExceeded {
			errorLog.Printf("Video thumbnail generation timed out after 60 seconds for %s", videoFilePath)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			errorLog.Printf("ffmpeg crashed or failed for %s: exit code %d, output: %s",
				videoFilePath, exitErr.ExitCode(), string(output))
		} else {
			errorLog.Printf("Video thumbnail generation failed for %s: %v, output: %s", videoFilePath, err, string(output))
		}
		return
	}

	// Check if temp file was created successfully
	if _, err := os.Stat(tempJpegPath); os.IsNotExist(err) {
		errorLog.Printf("Video conversion failed: output file not created")
		return
	}

	// Resize the video thumbnail
	resizedJpegPath, err := resizeJpegImage(tempJpegPath, standardImageWidth)
	if err != nil {
		errorLog.Printf("Error resizing video thumbnail: %v, using original size", err)
		// Continue with original size if resize fails
		resizedJpegPath = tempJpegPath
	} else {
		// Remove the original temp file if resize succeeded
		if err := os.Remove(tempJpegPath); err != nil && !os.IsNotExist(err) {
			errorLog.Printf("Warning: failed to remove intermediate temp file: %v", err)
		}
	}

	// Replace original file with resized JPEG thumbnail
	if err := os.Rename(resizedJpegPath, videoFilePath); err != nil {
		errorLog.Printf("Error replacing original video file: %v", err)
		return
	}

	// Don't cleanup temp file since we successfully renamed it
	cleanupTemp = false

	duration := time.Since(transformStart)
	infoLog.Printf("%sSuccessfully converted and resized video to JPEG thumbnail: %s [duration: %v]", bt.getQueueDepthString(), filepath.Base(videoFilePath), duration)
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

// hasVideoStream checks if a video file contains a video stream
// Uses ffprobe via exec (requires ffprobe to be available)
func (bt *BackupTransformer) hasVideoStream(videoFilePath string) bool {
	// Try to find ffprobe in project root, then PATH
	ffprobePath, found := findExecutable("ffprobe")
	if !found {
		// If ffprobe is not available, assume video stream exists and let ffmpeg handle the error
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_type",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoFilePath,
	}

	cmd := exec.CommandContext(ctx, ffprobePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If ffprobe fails, assume no video stream
		return false
	}

	outputStr := strings.TrimSpace(string(output))
	// Check if output contains "video" codec type
	return strings.Contains(outputStr, "video")
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
	defer func() {
		if err := file.Close(); err != nil {
			errorLog.Printf("Warning: error closing JPEG file: %v", err)
		}
	}()

	jpegImg, err := jpeg.Decode(file)
	if err != nil {
		return "", fmt.Errorf("failed to decode JPEG: %v", err)
	}

	// Resize the image
	resizedImg, err := resizeImage(jpegImg, maxWidth)
	if err != nil {
		return "", fmt.Errorf("failed to resize image: %v", err)
	}

	// Create temporary output file for resized JPEG
	tempResized, err := os.CreateTemp(filepath.Dir(jpegPath), "resized_*.jpg")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	resizedPath := tempResized.Name()
	if err := tempResized.Close(); err != nil {
		errorLog.Printf("Warning: error closing temp file: %v", err)
	}

	// Write resized JPEG
	resizedFile, err := os.Create(resizedPath)
	if err != nil {
		if rmErr := os.Remove(resizedPath); rmErr != nil && !os.IsNotExist(rmErr) {
			errorLog.Printf("Warning: failed to cleanup temp file: %v", rmErr)
		}
		return "", fmt.Errorf("failed to create resized file: %v", err)
	}

	// Ensure file is closed and handle errors
	var encodeErr error
	func() {
		defer func() {
			if err := resizedFile.Close(); err != nil {
				errorLog.Printf("Warning: error closing resized file: %v", err)
			}
		}()
		encodeErr = jpeg.Encode(resizedFile, resizedImg, &jpeg.Options{Quality: jpegQuality})
	}()

	if encodeErr != nil {
		if rmErr := os.Remove(resizedPath); rmErr != nil && !os.IsNotExist(rmErr) {
			errorLog.Printf("Warning: failed to cleanup temp file: %v", rmErr)
		}
		return "", fmt.Errorf("failed to encode resized JPEG: %v", encodeErr)
	}

	return resizedPath, nil
}
