package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ContentDetector analyzes files to determine their content type
type ContentDetector struct {
	signatures map[string]FileSignature
}

// FileSignature represents a file type signature
type FileSignature struct {
	Name        string
	Extension   string
	MagicBytes  [][]byte
	Offset      int
	Description string
}

// FileInfo contains detected information about a file
type FileInfo struct {
	Path        string
	Size        int64
	ContentType string
	Extension   string
	Description string
	Confidence  string
	Category    string
	Deletable   bool
	DeleteReason string
}

// FileAnalysisResult contains comprehensive analysis of a file
type FileAnalysisResult struct {
	FileInfo     *FileInfo
	IsSystemFile bool
	IsUserData   bool
	IsCache      bool
	IsTemporary  bool
	RiskLevel    string // "Low", "Medium", "High" for deletion risk
}

// NewContentDetector creates a new content detector with known file signatures
func NewContentDetector() *ContentDetector {
	detector := &ContentDetector{
		signatures: make(map[string]FileSignature),
	}
	detector.initializeSignatures()
	return detector
}

// initializeSignatures populates known file type signatures
func (cd *ContentDetector) initializeSignatures() {
	signatures := []FileSignature{
		// PDF
		{Name: "PDF", Extension: "pdf", MagicBytes: [][]byte{{0x25, 0x50, 0x44, 0x46}}, Offset: 0, Description: "Adobe PDF Document"},
		
		// SQLite
		{Name: "SQLite", Extension: "db", MagicBytes: [][]byte{{0x53, 0x51, 0x4C, 0x69, 0x74, 0x65, 0x20, 0x66, 0x6F, 0x72, 0x6D, 0x61, 0x74, 0x20, 0x33, 0x00}}, Offset: 0, Description: "SQLite Database"},
		
		// Images
		{Name: "JPEG", Extension: "jpg", MagicBytes: [][]byte{{0xFF, 0xD8, 0xFF}}, Offset: 0, Description: "JPEG Image"},
		{Name: "PNG", Extension: "png", MagicBytes: [][]byte{{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}}, Offset: 0, Description: "PNG Image"},
		{Name: "GIF", Extension: "gif", MagicBytes: [][]byte{{0x47, 0x49, 0x46, 0x38, 0x37, 0x61}, {0x47, 0x49, 0x46, 0x38, 0x39, 0x61}}, Offset: 0, Description: "GIF Image"},
		{Name: "HEIC", Extension: "heic", MagicBytes: [][]byte{{0x66, 0x74, 0x79, 0x70, 0x68, 0x65, 0x69, 0x63}}, Offset: 4, Description: "HEIC Image"},
		{Name: "WEBP", Extension: "webp", MagicBytes: [][]byte{{0x57, 0x45, 0x42, 0x50}}, Offset: 8, Description: "WEBP Image"},
		
		// Videos
		{Name: "MP4", Extension: "mp4", MagicBytes: [][]byte{{0x66, 0x74, 0x79, 0x70}}, Offset: 4, Description: "MP4 Video"},
		{Name: "MOV", Extension: "mov", MagicBytes: [][]byte{{0x66, 0x74, 0x79, 0x70, 0x71, 0x74}}, Offset: 4, Description: "QuickTime MOV Video"},
		{Name: "AVI", Extension: "avi", MagicBytes: [][]byte{{0x41, 0x56, 0x49, 0x20}}, Offset: 8, Description: "AVI Video"},
		{Name: "MPG", Extension: "mpg", MagicBytes: [][]byte{{0x00, 0x00, 0x01, 0xba}, {0x00, 0x00, 0x01, 0xb3}, {0x00, 0x00, 0x01, 0xb0}}, Offset: 0, Description: "MPEG Video"},
		{Name: "WMV", Extension: "wmv", MagicBytes: [][]byte{{0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11}}, Offset: 0, Description: "Windows Media Video"},
		{Name: "FLV", Extension: "flv", MagicBytes: [][]byte{{0x46, 0x4C, 0x56, 0x01}}, Offset: 0, Description: "Flash Video"},
		{Name: "WebM", Extension: "webm", MagicBytes: [][]byte{{0x1A, 0x45, 0xDF, 0xA3}}, Offset: 0, Description: "WebM Video"},
		{Name: "MKV", Extension: "mkv", MagicBytes: [][]byte{{0x1A, 0x45, 0xDF, 0xA3}}, Offset: 0, Description: "Matroska Video"},
		
		// Audio
		{Name: "MP3", Extension: "mp3", MagicBytes: [][]byte{{0x49, 0x44, 0x33}, {0xFF, 0xFB}, {0xFF, 0xF3}, {0xFF, 0xF2}}, Offset: 0, Description: "MP3 Audio"},
		{Name: "M4A", Extension: "m4a", MagicBytes: [][]byte{{0x66, 0x74, 0x79, 0x70, 0x4D, 0x34, 0x41}}, Offset: 4, Description: "M4A Audio"},
		{Name: "WAV", Extension: "wav", MagicBytes: [][]byte{{0x52, 0x49, 0x46, 0x46}}, Offset: 0, Description: "WAV Audio"},
		
		// Archives
		{Name: "ZIP", Extension: "zip", MagicBytes: [][]byte{{0x50, 0x4B, 0x03, 0x04}, {0x50, 0x4B, 0x05, 0x06}, {0x50, 0x4B, 0x07, 0x08}}, Offset: 0, Description: "ZIP Archive"},
		{Name: "GZIP", Extension: "gz", MagicBytes: [][]byte{{0x1F, 0x8B}}, Offset: 0, Description: "GZIP Archive"},
		
		// Text/Data
		{Name: "XML", Extension: "xml", MagicBytes: [][]byte{{0x3C, 0x3F, 0x78, 0x6D, 0x6C}}, Offset: 0, Description: "XML Document"},
		{Name: "JSON", Extension: "json", MagicBytes: [][]byte{{0x7B}, {0x5B}}, Offset: 0, Description: "JSON Data"},
		
		// iOS specific
		{Name: "PLIST", Extension: "plist", MagicBytes: [][]byte{{0x62, 0x70, 0x6C, 0x69, 0x73, 0x74}}, Offset: 0, Description: "Binary Property List"},
	}

	for _, sig := range signatures {
		cd.signatures[sig.Name] = sig
	}
}

// DetectFileType analyzes a file and returns its detected type information with enhanced analysis
func (cd *ContentDetector) DetectFileType(filePath string) (*FileInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// Get file info
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file stats: %v", err)
	}

	// Read first 64 bytes for magic number detection
	buffer := make([]byte, 64)
	n, err := file.Read(buffer)
	if err != nil && n == 0 {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}
	buffer = buffer[:n]

	// Detect content type
	contentType, confidence := cd.detectFromMagicBytes(buffer)
	
	// Fall back to extension-based detection if magic bytes failed
	if contentType == "Unknown" {
		extType := cd.detectFromExtension(filePath)
		if extType != "Unknown" {
			contentType = extType
			confidence = "Low (extension-based)"
		}
	}

	fileInfo := &FileInfo{
		Path:        filePath,
		Size:        stat.Size(),
		ContentType: contentType,
		Extension:   filepath.Ext(filePath),
		Description: cd.getDescription(contentType),
		Confidence:  confidence,
	}

	// Enhanced analysis for production use
	cd.enhanceWithHeuristics(fileInfo)

	return fileInfo, nil
}

// detectFromMagicBytes analyzes magic bytes to determine file type
func (cd *ContentDetector) detectFromMagicBytes(buffer []byte) (string, string) {
	for name, signature := range cd.signatures {
		for _, magicBytes := range signature.MagicBytes {
			if cd.matchesSignature(buffer, magicBytes, signature.Offset) {
				return name, "High (magic bytes)"
			}
		}
	}
	return "Unknown", "None"
}

// matchesSignature checks if buffer matches the signature at given offset
func (cd *ContentDetector) matchesSignature(buffer, signature []byte, offset int) bool {
	if len(buffer) < offset+len(signature) {
		return false
	}
	return bytes.Equal(buffer[offset:offset+len(signature)], signature)
}

// detectFromExtension attempts to detect file type from extension
func (cd *ContentDetector) detectFromExtension(filePath string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	
	for name, signature := range cd.signatures {
		if signature.Extension == ext {
			return name
		}
	}
	
	// Common extensions not in magic bytes
	extensionMap := map[string]string{
		"txt":  "Text",
		"log":  "Log File",
		"css":  "CSS",
		"js":   "JavaScript",
		"html": "HTML",
		"md":   "Markdown",
		"csv":  "CSV",
	}
	
	if contentType, exists := extensionMap[ext]; exists {
		return contentType
	}
	
	return "Unknown"
}

// getDescription returns a description for the detected content type
func (cd *ContentDetector) getDescription(contentType string) string {
	if signature, exists := cd.signatures[contentType]; exists {
		return signature.Description
	}
	
	descriptions := map[string]string{
		"Text":       "Plain Text File",
		"Log File":   "Log File",
		"CSS":        "Cascading Style Sheet",
		"JavaScript": "JavaScript File",
		"HTML":       "HTML Document",
		"Markdown":   "Markdown Document",
		"CSV":        "Comma Separated Values",
		"Unknown":    "Unknown File Type",
	}
	
	if desc, exists := descriptions[contentType]; exists {
		return desc
	}
	
	return "Unknown File Type"
}

// enhanceWithHeuristics adds intelligent categorization and deletability analysis
func (cd *ContentDetector) enhanceWithHeuristics(fileInfo *FileInfo) {
	fileName := strings.ToLower(filepath.Base(fileInfo.Path))
	dirPath := strings.ToLower(filepath.Dir(fileInfo.Path))
	
	// Initialize defaults
	fileInfo.Category = "Unknown"
	fileInfo.Deletable = false
	fileInfo.DeleteReason = ""

	// High-confidence deletable patterns
	if cd.isHighConfidenceDeletable(fileName, dirPath, fileInfo.Size, fileInfo.ContentType) {
		fileInfo.Deletable = true
		fileInfo.DeleteReason = "High confidence: Cache/temporary file"
		fileInfo.Category = "Cache/Temporary"
		return
	}

	// Medium-confidence deletable patterns
	if cd.isMediumConfidenceDeletable(fileName, dirPath, fileInfo.Size, fileInfo.ContentType) {
		fileInfo.Deletable = true
		fileInfo.DeleteReason = "Medium confidence: Likely cache/log file"
		fileInfo.Category = "Logs/Cache"
		return
	}

	// Categorize based on content type and patterns
	fileInfo.Category = cd.categorizeFile(fileInfo.ContentType, fileName, dirPath, fileInfo.Size)

	// Check for low-confidence deletable files
	if cd.isLowConfidenceDeletable(fileName, dirPath, fileInfo.Size, fileInfo.ContentType) {
		fileInfo.Deletable = true
		fileInfo.DeleteReason = "Low confidence: Review before deletion"
	}
}

// isHighConfidenceDeletable identifies files that are very likely safe to delete
func (cd *ContentDetector) isHighConfidenceDeletable(fileName, dirPath string, size int64, contentType string) bool {
	// Thumbnail files (iOS generates lots of these)
	if strings.Contains(fileName, "thumbnail") || strings.HasSuffix(fileName, ".ithmb") {
		return true
	}

	// Cache directories and files
	cachePatterns := []string{
		"cache", "caches", "tmp", "temp", "temporary",
		"preview", "previews", ".thumbnails",
	}
	for _, pattern := range cachePatterns {
		if strings.Contains(fileName, pattern) || strings.Contains(dirPath, pattern) {
			return true
		}
	}

	// Log files
	if strings.HasSuffix(fileName, ".log") || contentType == "Log File" {
		return true
	}

	// Analytics and diagnostic files
	diagnosticPatterns := []string{
		"analytics", "diagnostic", "crash", "crashlog",
		"usage", "metrics", "telemetry",
	}
	for _, pattern := range diagnosticPatterns {
		if strings.Contains(fileName, pattern) || strings.Contains(dirPath, pattern) {
			return true
		}
	}

	// Small system files that are likely preferences or state
	if size < 1024 && contentType == "PLIST" { // Files smaller than 1KB
		return true
	}

	// Backup-specific temporary files
	backupTempPatterns := []string{
		"manifest.db-shm", "manifest.db-wal", ".partial", ".downloading",
	}
	for _, pattern := range backupTempPatterns {
		if strings.Contains(fileName, pattern) {
			return true
		}
	}

	return false
}

// isMediumConfidenceDeletable identifies files that are probably safe to delete
func (cd *ContentDetector) isMediumConfidenceDeletable(fileName, dirPath string, size int64, contentType string) bool {
	// Large unknown files (likely cache or temporary data)
	if contentType == "Unknown" && size > 10*1024*1024 { // > 10MB
		return true
	}

	// System state files that are regenerable
	statePatterns := []string{
		"state", "status", "index", "queue",
	}
	for _, pattern := range statePatterns {
		if strings.Contains(fileName, pattern) && contentType == "PLIST" {
			return true
		}
	}

	// Database files in cache-like directories
	if contentType == "SQLite" && (strings.Contains(dirPath, "cache") || 
		strings.Contains(dirPath, "temp") || size < 1024*1024) { // < 1MB SQLite files
		return true
	}

	return false
}

// isLowConfidenceDeletable identifies files that might be safe to delete but need review
func (cd *ContentDetector) isLowConfidenceDeletable(fileName, dirPath string, size int64, contentType string) bool {
	// Very small unknown files
	if contentType == "Unknown" && size < 100 {
		return true
	}

	// Large log files
	if contentType == "Log File" && size > 1024*1024 { // > 1MB
		return true
	}

	return false
}

// categorizeFile provides intelligent categorization based on content and patterns
func (cd *ContentDetector) categorizeFile(contentType, fileName, dirPath string, size int64) string {
	// Media files
	mediaTypes := []string{"JPEG", "PNG", "GIF", "HEIC", "WEBP", "MP4", "MOV", "M4A", "MP3"}
	for _, mediaType := range mediaTypes {
		if contentType == mediaType {
			if size > 1024*1024 { // > 1MB
				return "User Media (Photos/Videos)"
			}
			return "Media Files"
		}
	}

	// Documents
	if contentType == "PDF" {
		return "Documents"
	}

	// App data
	if contentType == "SQLite" {
		if size > 10*1024*1024 { // > 10MB
			return "Large Database (App Data)"
		}
		return "Database Files"
	}

	// System files
	if contentType == "PLIST" {
		if strings.Contains(fileName, "pref") || strings.Contains(dirPath, "preferences") {
			return "App Preferences"
		}
		return "System Configuration"
	}

	// Web content
	webTypes := []string{"HTML", "CSS", "JavaScript", "JSON"}
	for _, webType := range webTypes {
		if contentType == webType {
			return "Web Content"
		}
	}

	// Archives
	if contentType == "ZIP" || contentType == "GZIP" {
		return "Archive Files"
	}

	// Text files
	if contentType == "Text" || contentType == "Log File" {
		return "Text/Log Files"
	}

	return "Unclassified"
}

// AnalyzeForDeletion provides a comprehensive analysis for deletion decisions
func (cd *ContentDetector) AnalyzeForDeletion(filePath string) (*FileAnalysisResult, error) {
	fileInfo, err := cd.DetectFileType(filePath)
	if err != nil {
		return nil, err
	}

	result := &FileAnalysisResult{
		FileInfo: fileInfo,
	}

	// Determine file characteristics
	result.IsCache = strings.Contains(strings.ToLower(filePath), "cache") || 
		strings.Contains(strings.ToLower(filePath), "tmp") ||
		fileInfo.Category == "Cache/Temporary"

	result.IsTemporary = strings.Contains(strings.ToLower(filePath), "temp") ||
		strings.HasSuffix(strings.ToLower(filePath), ".tmp")

	result.IsSystemFile = fileInfo.ContentType == "PLIST" || 
		fileInfo.ContentType == "SQLite" ||
		strings.Contains(fileInfo.Category, "System")

	result.IsUserData = strings.Contains(fileInfo.Category, "User Media") ||
		strings.Contains(fileInfo.Category, "Documents")

	// Determine risk level
	if fileInfo.Deletable && fileInfo.DeleteReason == "High confidence: Cache/temporary file" {
		result.RiskLevel = "Low"
	} else if fileInfo.Deletable && fileInfo.DeleteReason == "Medium confidence: Likely cache/log file" {
		result.RiskLevel = "Medium"
	} else if fileInfo.Deletable {
		result.RiskLevel = "High"
	} else if result.IsUserData {
		result.RiskLevel = "Critical" // Never delete user data
	} else {
		result.RiskLevel = "High" // Conservative approach
	}

	return result, nil
}

// GetDeletionSummary provides a summary of what can be safely deleted
func (cd *ContentDetector) GetDeletionSummary(directory string) (map[string]int64, error) {
	summary := map[string]int64{
		"HighConfidenceDeletable": 0,
		"MediumConfidenceDeletable": 0,
		"LowConfidenceDeletable": 0,
		"KeepUserData": 0,
		"KeepSystemCritical": 0,
	}

	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		result, err := cd.AnalyzeForDeletion(path)
		if err != nil {
			return nil // Continue on errors
		}

		switch result.RiskLevel {
		case "Low":
			summary["HighConfidenceDeletable"] += info.Size()
		case "Medium":
			summary["MediumConfidenceDeletable"] += info.Size()
		case "High":
			if result.FileInfo.Deletable {
				summary["LowConfidenceDeletable"] += info.Size()
			} else {
				summary["KeepSystemCritical"] += info.Size()
			}
		case "Critical":
			summary["KeepUserData"] += info.Size()
		}

		return nil
	})

	return summary, err
}