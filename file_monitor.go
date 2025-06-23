package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileMonitor monitors a directory for new files and analyzes them
type FileMonitor struct {
	watchDir         string
	outputFile       string
	detector         *ContentDetector
	manifestAnalyzer *ManifestAnalyzer
	watcher          *fsnotify.Watcher
	outputMutex      sync.Mutex
	processedFiles   map[string]time.Time
	mu               sync.Mutex
	scanExisting     bool
}

// NewFileMonitor creates a new file monitor
func NewFileMonitor(watchDir, outputFile string, detector *ContentDetector, manifestAnalyzer *ManifestAnalyzer, scanExisting bool) (*FileMonitor, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %v", err)
	}

	monitor := &FileMonitor{
		watchDir:         watchDir,
		outputFile:       outputFile,
		detector:         detector,
		manifestAnalyzer: manifestAnalyzer,
		watcher:          watcher,
		processedFiles:   make(map[string]time.Time),
		scanExisting:     scanExisting,
	}

	return monitor, nil
}

// Start begins monitoring the directory
func (fm *FileMonitor) Start() error {
	// Add the watch directory
	err := fm.watcher.Add(fm.watchDir)
	if err != nil {
		return fmt.Errorf("failed to add watch directory: %v", err)
	}

	// Also watch subdirectories
	err = filepath.Walk(fm.watchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fm.watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		log.Printf("Warning: failed to add some subdirectories to watch: %v", err)
	}

	// Write header to output file
	fm.writeHeader()

	// Scan existing files if requested
	if fm.scanExisting {
		fmt.Printf("Scanning existing files in %s...\n", fm.watchDir)
		go fm.scanExistingFiles()
	}

	// Start the event processing goroutine
	go fm.processEvents()

	return nil
}

// processEvents handles file system events
func (fm *FileMonitor) processEvents() {
	for {
		select {
		case event, ok := <-fm.watcher.Events:
			if !ok {
				return
			}
			fm.handleEvent(event)

		case err, ok := <-fm.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// handleEvent processes individual file system events
func (fm *FileMonitor) handleEvent(event fsnotify.Event) {
	// Only process CREATE and WRITE events
	if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) {
		return
	}

	// Skip directories
	if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
		// If it's a new directory, add it to the watcher
		if event.Has(fsnotify.Create) {
			fm.watcher.Add(event.Name)
		}
		return
	}

	// Check if we've already processed this file recently
	fm.mu.Lock()
	lastProcessed, exists := fm.processedFiles[event.Name]
	now := time.Now()
	
	// Only process if file hasn't been processed in the last 2 seconds
	// This helps avoid duplicate processing for rapid file events
	if exists && now.Sub(lastProcessed) < 2*time.Second {
		fm.mu.Unlock()
		return
	}
	
	fm.processedFiles[event.Name] = now
	fm.mu.Unlock()

	// Process the file with a small delay to ensure file is completely written
	go func(filename string) {
		time.Sleep(100 * time.Millisecond)
		fm.processFile(filename)
	}(event.Name)
}

// processFile analyzes a file and writes the results
func (fm *FileMonitor) processFile(filePath string) {
	// Skip if file no longer exists (might have been temporary)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return
	}

	// Detect file type using content detector
	fileInfo, err := fm.detector.DetectFileType(filePath)
	if err != nil {
		log.Printf("Error detecting file type for %s: %v", filePath, err)
		return
	}

	// Enhance with manifest information if available
	var manifestInfo *FileManifestInfo
	if fm.manifestAnalyzer != nil {
		fileHash := ExtractFileHashFromPath(filePath)
		manifestInfo, err = fm.manifestAnalyzer.GetFileInfo(fileHash)
		if err != nil {
			log.Printf("Error getting manifest info for %s: %v", fileHash, err)
		}
	}

	// Write results to output file
	fm.writeResults(fileInfo, manifestInfo)

	// Log to console with enhanced information
	categoryInfo := ""
	deletableInfo := ""
	
	if manifestInfo != nil {
		// Use manifest information when available
		categoryInfo = fmt.Sprintf(" [%s: %s]", manifestInfo.AppName, manifestInfo.FileCategory)
		if manifestInfo.Deletable {
			deletableInfo = " (DELETABLE)"
		}
	} else {
		// Use enhanced content detector analysis when manifest not available
		if fileInfo.Category != "Unknown" && fileInfo.Category != "" {
			categoryInfo = fmt.Sprintf(" [%s]", fileInfo.Category)
		}
		if fileInfo.Deletable {
			deletableInfo = fmt.Sprintf(" (DELETABLE: %s)", fileInfo.DeleteReason)
		}
	}

	fmt.Printf("[%s] Detected: %s - %s (%s) - Size: %s%s%s\n",
		time.Now().Format("15:04:05"),
		filepath.Base(fileInfo.Path),
		fileInfo.ContentType,
		fileInfo.Description,
		formatFileSize(fileInfo.Size),
		categoryInfo,
		deletableInfo,
	)
}

// writeHeader writes the header to the output file
func (fm *FileMonitor) writeHeader() {
	fm.outputMutex.Lock()
	defer fm.outputMutex.Unlock()

	file, err := os.Create(fm.outputFile)
	if err != nil {
		log.Printf("Error creating output file: %v", err)
		return
	}
	defer file.Close()

	analysisType := "Smart File Analysis (Production Mode)"
	if fm.manifestAnalyzer != nil {
		analysisType = "Enhanced File Analysis (with Manifest)"
	}

	header := fmt.Sprintf("iOS Backup %s - Started at %s\n", analysisType, time.Now().Format("2006-01-02 15:04:05"))
	header += strings.Repeat("=", len(header)-1) + "\n"

	if fm.manifestAnalyzer != nil {
		// Enhanced header with manifest columns
		header += fmt.Sprintf("%-20s %-15s %-30s %-15s %-12s %-20s %-25s %-8s %-40s %s\n",
			"Timestamp", "Content Type", "Description", "Confidence", "Size", "App Name", "Category", "Deletable", "Original Path", "Backup Path")
		header += strings.Repeat("-", 200) + "\n"
	} else {
		// Production header with smart analysis columns
		header += fmt.Sprintf("%-20s %-15s %-30s %-15s %-12s %-25s %-8s %-40s %s\n",
			"Timestamp", "Content Type", "Description", "Confidence", "Size", "Category", "Deletable", "Delete Reason", "File Path")
		header += strings.Repeat("-", 180) + "\n"
	}

	_, err = file.WriteString(header)
	if err != nil {
		log.Printf("Error writing header: %v", err)
	}
}

// writeResults appends analysis results to the output file
func (fm *FileMonitor) writeResults(fileInfo *FileInfo, manifestInfo *FileManifestInfo) {
	fm.outputMutex.Lock()
	defer fm.outputMutex.Unlock()

	file, err := os.OpenFile(fm.outputFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening output file: %v", err)
		return
	}
	defer file.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	relativePath := fm.getRelativePath(fileInfo.Path)
	
	// Enhanced output with manifest information if available
	if manifestInfo != nil {
		appName := truncateString(manifestInfo.AppName, 20)
		category := truncateString(manifestInfo.FileCategory, 25)
		deletable := "No"
		if manifestInfo.Deletable {
			deletable = "Yes"
		}
		originalPath := truncateString(manifestInfo.RelativePath, 40)
		
		line := fmt.Sprintf("%-20s %-15s %-30s %-15s %-12s %-20s %-25s %-8s %-40s %s\n",
			timestamp,
			fileInfo.ContentType,
			truncateString(fileInfo.Description, 30),
			fileInfo.Confidence,
			formatFileSize(fileInfo.Size),
			appName,
			category,
			deletable,
			originalPath,
			relativePath,
		)
		_, err = file.WriteString(line)
	} else {
		// Production format with smart analysis when no manifest info available
		category := truncateString(fileInfo.Category, 25)
		deletable := "No"
		if fileInfo.Deletable {
			deletable = "Yes"
		}
		deleteReason := truncateString(fileInfo.DeleteReason, 40)
		
		line := fmt.Sprintf("%-20s %-15s %-30s %-15s %-12s %-25s %-8s %-40s %s\n",
			timestamp,
			fileInfo.ContentType,
			truncateString(fileInfo.Description, 30),
			fileInfo.Confidence,
			formatFileSize(fileInfo.Size),
			category,
			deletable,
			deleteReason,
			relativePath,
		)
		_, err = file.WriteString(line)
	}

	if err != nil {
		log.Printf("Error writing results: %v", err)
	}
}

// getRelativePath returns the path relative to the watch directory
func (fm *FileMonitor) getRelativePath(fullPath string) string {
	relPath, err := filepath.Rel(fm.watchDir, fullPath)
	if err != nil {
		return fullPath
	}
	return relPath
}

// formatFileSize formats file size in human-readable format
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// truncateString truncates a string to specified length with ellipsis
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// scanExistingFiles scans and processes all existing files in the watch directory
func (fm *FileMonitor) scanExistingFiles() {
	fileCount := 0
	startTime := time.Now()

	err := filepath.Walk(fm.watchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Error accessing path %s: %v", path, err)
			return nil // Continue walking despite errors
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip hidden files and common backup/temporary files
		baseName := filepath.Base(path)
		if strings.HasPrefix(baseName, ".") || 
		   strings.HasSuffix(baseName, ".tmp") || 
		   strings.HasSuffix(baseName, ".temp") {
			return nil
		}

		// Process the file
		fm.processFile(path)
		fileCount++

		// Add small delay to avoid overwhelming the system
		if fileCount%50 == 0 {
			time.Sleep(10 * time.Millisecond)
		}

		return nil
	})

	duration := time.Since(startTime)
	if err != nil {
		log.Printf("Error during initial scan: %v", err)
	}

	fmt.Printf("Initial scan completed: %d files processed in %v\n", fileCount, duration)
}

// Close stops the file monitor
func (fm *FileMonitor) Close() error {
	return fm.watcher.Close()
} 