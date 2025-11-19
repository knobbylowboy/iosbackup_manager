package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	// Define command line flags
	var (
		watchDir       = flag.String("dir", "", "Directory to monitor for new files (required)")
		outputFile     = flag.String("output", "file_analysis.txt", "Output file for analysis results")
		scanExisting   = flag.Bool("scan-existing", false, "Scan and process existing files in the directory")
		useManifest    = flag.Bool("use-manifest", false, "Use Manifest.db for enhanced file identification")
		manifestPath   = flag.String("manifest", "", "Path to Manifest.db file (auto-detected if not specified)")
		generateSummary = flag.Bool("deletion-summary", false, "Generate a deletion safety summary and exit (no monitoring)")
		help           = flag.Bool("help", false, "Show usage information")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "iOS Backup File Monitor - Monitors directories for new files and identifies content types\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s -dir <watch_directory> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -dir /path/to/ios/backup\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -dir /path/to/ios/backup -output analysis.txt\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -dir /path/to/ios/backup -scan-existing\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -dir /path/to/ios/backup -use-manifest\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -dir /path/to/ios/backup -deletion-summary\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -dir /path/to/ios/backup -use-manifest -manifest /path/to/Manifest.db\n", os.Args[0])
	}

	flag.Parse()

	if *help || *watchDir == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Validate watch directory exists
	if _, err := os.Stat(*watchDir); os.IsNotExist(err) {
		log.Fatalf("Watch directory does not exist: %s", *watchDir)
	}

	// Initialize the content detector
	detector := NewContentDetector()

	// Initialize manifest analyzer if requested
	var manifestAnalyzer *ManifestAnalyzer
	if *useManifest {
		manifestDbPath := *manifestPath
		if manifestDbPath == "" {
			// Auto-detect manifest path
			manifestDbPath = filepath.Join(*watchDir, "Manifest.db")
		}
		
		if _, err := os.Stat(manifestDbPath); err == nil {
			manifestAnalyzer, err = NewManifestAnalyzer(manifestDbPath)
			if err != nil {
				log.Printf("Warning: Failed to initialize manifest analyzer: %v", err)
				manifestAnalyzer = nil
			} else {
				defer manifestAnalyzer.Close()
				fmt.Printf("Manifest analyzer enabled: %s\n", manifestDbPath)
			}
		} else {
			log.Printf("Warning: Manifest.db not found at %s, continuing without manifest analysis", manifestDbPath)
		}
	}

	// If deletion summary requested, generate and exit
	if *generateSummary {
		fmt.Printf("Generating deletion safety summary for: %s\n", *watchDir)
		fmt.Printf("Analysis mode: %s\n\n", getAnalysisMode(manifestAnalyzer))
		
		err := generateDeletionSummary(detector, *watchDir)
		if err != nil {
			log.Fatalf("Failed to generate deletion summary: %v", err)
		}
		return
	}

	// Initialize the file monitor
	monitor, err := NewFileMonitor(*watchDir, *outputFile, detector, manifestAnalyzer, *scanExisting)
	if err != nil {
		log.Fatalf("Failed to initialize file monitor: %v", err)
	}
	defer monitor.Close()

	fmt.Printf("Starting iOS backup file monitor...\n")
	fmt.Printf("Watching directory: %s\n", *watchDir)
	fmt.Printf("Output file: %s\n", *outputFile)
	fmt.Printf("Analysis mode: %s\n", getAnalysisMode(manifestAnalyzer))
	fmt.Printf("Scan existing files: %t\n", *scanExisting)
	fmt.Printf("Use manifest: %t\n", *useManifest)
	fmt.Printf("Press Ctrl+C to stop\n\n")

	// Start monitoring
	if err := monitor.Start(); err != nil {
		log.Fatalf("Failed to start monitoring: %v", err)
	}

	// Keep the application running
	select {}
}

// getAnalysisMode returns a description of the current analysis mode
func getAnalysisMode(manifestAnalyzer *ManifestAnalyzer) string {
	if manifestAnalyzer != nil {
		return "Enhanced (with Manifest Database)"
	}
	return "Production Smart Analysis (without Manifest)"
}

// generateDeletionSummary creates a comprehensive deletion safety report
func generateDeletionSummary(detector *ContentDetector, directory string) error {
	summary, err := detector.GetDeletionSummary(directory)
	if err != nil {
		return fmt.Errorf("failed to analyze directory: %v", err)
	}

	// Calculate totals
	totalSize := int64(0)
	safeToDeleteSize := int64(0)
	
	for category, size := range summary {
		totalSize += size
		if category == "HighConfidenceDeletable" || category == "MediumConfidenceDeletable" {
			safeToDeleteSize += size
		}
	}

	// Display results
	fmt.Printf("=== DELETION SAFETY SUMMARY ===\n\n")
	
	fmt.Printf("Total backup size: %s\n", formatFileSize(totalSize))
	fmt.Printf("Safe to delete: %s (%.1f%%)\n\n", 
		formatFileSize(safeToDeleteSize), 
		float64(safeToDeleteSize)/float64(totalSize)*100)

	fmt.Printf("Detailed breakdown:\n")
	fmt.Printf("• High confidence deletable: %s (cache, thumbnails, logs)\n", 
		formatFileSize(summary["HighConfidenceDeletable"]))
	fmt.Printf("• Medium confidence deletable: %s (large unknown files, temp databases)\n", 
		formatFileSize(summary["MediumConfidenceDeletable"]))
	fmt.Printf("• Low confidence deletable: %s (review recommended)\n", 
		formatFileSize(summary["LowConfidenceDeletable"]))
	fmt.Printf("• User data (KEEP): %s (photos, videos, documents)\n", 
		formatFileSize(summary["KeepUserData"]))
	fmt.Printf("• System critical (KEEP): %s (app data, preferences)\n", 
		formatFileSize(summary["KeepSystemCritical"]))

	fmt.Printf("\n=== RECOMMENDATIONS ===\n")
	fmt.Printf("✅ SAFE TO DELETE: Files marked as 'High confidence deletable'\n")
	fmt.Printf("⚠️  PROBABLY SAFE: Files marked as 'Medium confidence deletable'\n")
	fmt.Printf("🔍 REVIEW FIRST: Files marked as 'Low confidence deletable'\n")
	fmt.Printf("❌ DO NOT DELETE: User data and system critical files\n")

	if safeToDeleteSize > 0 {
		fmt.Printf("\n💾 Potential space savings: %s\n", formatFileSize(safeToDeleteSize))
	}

	return nil
} 