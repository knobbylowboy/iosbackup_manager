package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// ManifestAnalyzer provides functionality to query iOS backup manifest databases
type ManifestAnalyzer struct {
	db *sql.DB
}

// FileManifestInfo contains information from the iOS backup manifest
type FileManifestInfo struct {
	FileID       string
	Domain       string
	RelativePath string
	AppName      string
	FileCategory string
	Deletable    bool
	Confidence   string
}

// NewManifestAnalyzer creates a new manifest analyzer
func NewManifestAnalyzer(manifestPath string) (*ManifestAnalyzer, error) {
	db, err := sql.Open("sqlite3", manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest database: %v", err)
	}

	// Test the connection
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM Files").Scan(&count)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to query manifest database: %v", err)
	}

	return &ManifestAnalyzer{db: db}, nil
}

// GetFileInfo retrieves manifest information for a given file hash
func (ma *ManifestAnalyzer) GetFileInfo(fileHash string) (*FileManifestInfo, error) {
	query := "SELECT fileID, domain, relativePath FROM Files WHERE fileID = ?"
	row := ma.db.QueryRow(query, fileHash)

	var info FileManifestInfo
	err := row.Scan(&info.FileID, &info.Domain, &info.RelativePath)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // File not found in manifest
		}
		return nil, fmt.Errorf("failed to query file info: %v", err)
	}

	// Analyze the domain and path to determine app name, category, and deletability
	ma.analyzeFileInfo(&info)

	return &info, nil
}

// analyzeFileInfo determines app name, category, and deletability based on domain and path
func (ma *ManifestAnalyzer) analyzeFileInfo(info *FileManifestInfo) {
	// Extract app name from domain
	info.AppName = ma.extractAppName(info.Domain)
	
	// Determine file category and deletability
	info.FileCategory, info.Deletable, info.Confidence = ma.categorizeFile(info.Domain, info.RelativePath)
}

// extractAppName extracts a human-readable app name from the domain
func (ma *ManifestAnalyzer) extractAppName(domain string) string {
	switch {
	case strings.Contains(domain, "com.apple."):
		if strings.Contains(domain, "mobilesafari") {
			return "Safari"
		} else if strings.Contains(domain, "Photos") {
			return "Photos"
		} else if strings.Contains(domain, "PosterBoard") {
			return "Wallpapers"
		}
		return "Apple System"
	case strings.Contains(domain, "com.google."):
		if strings.Contains(domain, "photos") {
			return "Google Photos"
		}
		return "Google"
	case domain == "MediaDomain":
		return "Media/SMS"
	case domain == "CameraRollDomain":
		return "Camera Roll"
	case domain == "HomeDomain":
		return "Home Screen"
	case strings.HasPrefix(domain, "AppDomain-"):
		// Extract app identifier from AppDomain-com.company.app
		appID := strings.TrimPrefix(domain, "AppDomain-")
		parts := strings.Split(appID, ".")
		if len(parts) > 0 {
			return parts[len(parts)-1] // Return the last part (app name)
		}
		return appID
	default:
		return domain
	}
}

// categorizeFile determines the file category and whether it's safe to delete
func (ma *ManifestAnalyzer) categorizeFile(domain, relativePath string) (string, bool, string) {
	path := strings.ToLower(relativePath)
	
	// High confidence deletable files
	if strings.Contains(path, "cache") || strings.Contains(path, "tmp") || 
	   strings.Contains(path, "thumbnails") || strings.Contains(path, "preview") ||
	   strings.HasSuffix(path, ".ithmb") || strings.Contains(path, "temp") {
		return "Cache/Temporary", true, "High"
	}

	// Specific file types that are usually safe to delete
	if strings.HasSuffix(path, ".log") || strings.Contains(path, "analytics") ||
	   strings.Contains(path, "crash") || strings.Contains(path, "diagnostic") {
		return "Logs/Diagnostics", true, "High"
	}

	// Media thumbnails and cache
	if domain == "CameraRollDomain" && strings.Contains(path, "thumbnails") {
		return "Photo Thumbnails", true, "High"
	}

	// App-specific analysis
	switch domain {
	case "MediaDomain":
		if strings.Contains(path, "sms/attachments") {
			return "SMS Attachments", false, "High" // Don't delete message attachments
		}
		return "Media Files", false, "Medium"
		
	case "CameraRollDomain":
		if strings.Contains(path, "dcim") || strings.Contains(path, "media/photodb") {
			return "Photos/Videos", false, "High" // Don't delete actual photos
		}
		return "Camera Roll Data", false, "Medium"
		
	case "HomeDomain":
		if strings.Contains(path, "library/preferences") {
			return "App Preferences", false, "Medium" // Don't delete user settings
		}
		return "Home Screen Data", false, "Medium"
	}

	// App domains
	if strings.HasPrefix(domain, "AppDomain-") {
		if strings.Contains(path, "documents") {
			return "App Documents", false, "High" // Don't delete user documents
		} else if strings.Contains(path, "library/caches") || strings.Contains(path, "library/application support/cache") {
			return "App Cache", true, "High"
		} else if strings.Contains(path, "library/preferences") {
			return "App Settings", false, "Medium"
		}
		return "App Data", false, "Low"
	}

	// Default case
	return "System Data", false, "Low"
}

// GetDeletableFiles returns a summary of files that can be safely deleted
func (ma *ManifestAnalyzer) GetDeletableFiles() ([]FileManifestInfo, error) {
	query := `SELECT fileID, domain, relativePath FROM Files WHERE 
		LOWER(relativePath) LIKE '%cache%' OR 
		LOWER(relativePath) LIKE '%tmp%' OR 
		LOWER(relativePath) LIKE '%thumbnails%' OR 
		LOWER(relativePath) LIKE '%.ithmb' OR
		LOWER(relativePath) LIKE '%temp%' OR
		LOWER(relativePath) LIKE '%.log'`
	
	rows, err := ma.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query deletable files: %v", err)
	}
	defer rows.Close()

	var deletableFiles []FileManifestInfo
	for rows.Next() {
		var info FileManifestInfo
		err := rows.Scan(&info.FileID, &info.Domain, &info.RelativePath)
		if err != nil {
			continue
		}
		ma.analyzeFileInfo(&info)
		if info.Deletable {
			deletableFiles = append(deletableFiles, info)
		}
	}

	return deletableFiles, nil
}

// GetDomainSummary returns a summary of files by domain
func (ma *ManifestAnalyzer) GetDomainSummary() (map[string]int, error) {
	query := "SELECT domain, COUNT(*) as count FROM Files GROUP BY domain ORDER BY count DESC"
	rows, err := ma.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain summary: %v", err)
	}
	defer rows.Close()

	summary := make(map[string]int)
	for rows.Next() {
		var domain string
		var count int
		err := rows.Scan(&domain, &count)
		if err != nil {
			continue
		}
		summary[domain] = count
	}

	return summary, nil
}

// Close closes the database connection
func (ma *ManifestAnalyzer) Close() error {
	return ma.db.Close()
}

// ExtractFileHashFromPath extracts the file hash from a backup file path
func ExtractFileHashFromPath(filePath string) string {
	// iOS backup files are stored as hash/hash (e.g., "ab/abcd1234...")
	dir := filepath.Dir(filePath)
	filename := filepath.Base(filePath)
	
	// The filename should be the full hash
	if len(filename) == 40 { // SHA-1 hash length
		return filename
	}
	
	// If not, try to extract from the directory structure
	dirName := filepath.Base(dir)
	if len(dirName) == 2 && len(filename) > 2 {
		// Assume the structure is XX/XXYYY... where XX is repeated
		return dirName + filename
	}
	
	return filename
} 