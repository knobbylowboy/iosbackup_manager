package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var (
		watchDir = flag.String("dir", "", "Directory to monitor for new files (required)")
		help     = flag.Bool("help", false, "Show usage information")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "iOS Backup Transformer - Monitors directories and converts backup files\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s -dir <watch_directory>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -dir /path/to/ios/backup\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nSupported file types:\n")
		fmt.Fprintf(os.Stderr, "  - HEIC images -> JPEG (overwrites original, requires heic-converter)\n")
		fmt.Fprintf(os.Stderr, "  - GIF images -> JPEG (overwrites original, uses embedded Go library)\n")
		fmt.Fprintf(os.Stderr, "  - Video files (MP4, MOV, AVI) -> JPEG thumbnail (overwrites original, requires ffmpeg)\n")
		fmt.Fprintf(os.Stderr, "\nNote: HEIC and video conversion require external tools (heic-converter, ffmpeg, ffprobe)\n")
		fmt.Fprintf(os.Stderr, "      to be available in the project root (same directory as this executable) or PATH.\n")
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

	// Create backup transformer (no external executable paths needed)
	transformer := NewBackupTransformer()

	// Create file monitor
	monitor, err := NewBackupFileMonitor(*watchDir, transformer)
	if err != nil {
		log.Fatalf("Failed to initialize file monitor: %v", err)
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Printf("Starting iOS backup transformer...\n")
	fmt.Printf("Watching directory: %s\n", *watchDir)
	fmt.Printf("GIF conversion: Embedded (pure Go)\n")
	fmt.Printf("HEIC conversion: Requires heic-converter in project root or PATH\n")
	fmt.Printf("Video conversion: Requires ffmpeg/ffprobe in project root or PATH\n")
	fmt.Printf("Press Ctrl+C or send SIGTERM to stop\n\n")

	// Start monitoring
	if err := monitor.Start(); err != nil {
		log.Fatalf("Failed to start monitoring: %v", err)
	}

	// Wait for shutdown signal
	<-sigChan
	fmt.Println("\nShutting down gracefully...")
	monitor.Stop()
	fmt.Println("Shutdown complete")
}

