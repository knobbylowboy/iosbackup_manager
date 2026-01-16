package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var (
	// infoLog writes informational messages to stdout
	infoLog *log.Logger
	// errorLog writes error messages to stderr
	errorLog *log.Logger
)

func main() {
	var (
		backupDir  = flag.String("backup-dir", "", "Backup directory path (required)")
		iosBackup  = flag.String("ios-backup", "ios_backup", "Path to ios_backup executable")
		verbose    = flag.Bool("verbose", false, "Show verbose output including filtered files")
		logFile    = flag.String("log-file", "", "Save output to a log file (optional)")
		help       = flag.Bool("help", false, "Show usage information")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "iOS Backup Transformer - Runs ios_backup and converts media files during backup\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s -backup-dir <backup_directory>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Description:\n")
		fmt.Fprintf(os.Stderr, "  This tool runs ios_backup (modified idevicebackup2) that filters files by domain.\n")
		fmt.Fprintf(os.Stderr, "  It parses the ios_backup output and transforms media files as they are saved.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -backup-dir /path/to/ios/backup/00008110-000E785101F2401E\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -backup-dir /path/to/ios/backup/00008110-000E785101F2401E -verbose\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -backup-dir /path/to/ios/backup/00008110-000E785101F2401E -log-file backup.log\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nDomain filters (automatically applied):\n")
		fmt.Fprintf(os.Stderr, "  - *SMS* / *sms* - Text messages\n")
		fmt.Fprintf(os.Stderr, "  - *AddressBook* - Contacts\n")
		fmt.Fprintf(os.Stderr, "  - *WhatsApp* / *whatsapp* - WhatsApp data\n")
		fmt.Fprintf(os.Stderr, "  - *ChatStorage.sqlite* - WhatsApp chat database\n")
		fmt.Fprintf(os.Stderr, "  - *Message/Media/* - WhatsApp media files\n")
		fmt.Fprintf(os.Stderr, "\nMedia transformations:\n")
		fmt.Fprintf(os.Stderr, "  - HEIC images -> JPEG (resized to 500px width, requires heic-converter)\n")
		fmt.Fprintf(os.Stderr, "  - GIF images -> JPEG (resized to 500px width, pure Go)\n")
		fmt.Fprintf(os.Stderr, "  - PNG images -> JPEG (resized to 500px width, pure Go)\n")
		fmt.Fprintf(os.Stderr, "  - WEBP images -> JPEG (resized to 500px width, pure Go)\n")
		fmt.Fprintf(os.Stderr, "  - JPEG images -> resized JPEG (500px width, pure Go)\n")
		fmt.Fprintf(os.Stderr, "  - Videos (MP4, MOV, AVI, etc.) -> JPEG thumbnail (requires ffmpeg/ffprobe)\n")
		fmt.Fprintf(os.Stderr, "\nNote: HEIC and video conversion require external tools (heic-converter, ffmpeg, ffprobe)\n")
		fmt.Fprintf(os.Stderr, "      to be available in libraries folder, project root, or PATH.\n")
	}

	flag.Parse()

	if *help || *backupDir == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Set up log file if specified
	var logFileHandle *os.File
	var err error
	if *logFile != "" {
		logFileHandle, err = os.Create(*logFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create log file: %v\n", err)
			os.Exit(1)
		}
		defer logFileHandle.Close()
		
		// Create multi-writers to output to both console and file
		infoWriter := io.MultiWriter(os.Stdout, logFileHandle)
		errorWriter := io.MultiWriter(os.Stderr, logFileHandle)
		
		infoLog = log.New(infoWriter, "", 0)
		errorLog = log.New(errorWriter, "", 0)
		
		// Replace standard log with errorLog for backward compatibility
		log.SetOutput(errorWriter)
		log.SetFlags(0)
		
		fmt.Fprintf(logFileHandle, "iOS Backup Transformer - Log started at %s\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(logFileHandle, "Backup directory: %s\n", *backupDir)
		fmt.Fprintf(logFileHandle, "Verbose: %v\n\n", *verbose)
	} else {
		// Initialize loggers: info to stdout, errors to stderr
		infoLog = log.New(os.Stdout, "", 0)
		errorLog = log.New(os.Stderr, "", 0)
		
		// Replace standard log with errorLog for backward compatibility with log.Fatalf
		log.SetOutput(os.Stderr)
		log.SetFlags(0)
	}

	// Validate backup directory parent exists (backup dir itself may not exist yet)
	backupParent := filepath.Dir(*backupDir)
	if _, err := os.Stat(backupParent); os.IsNotExist(err) {
		errorLog.Printf("Backup directory parent does not exist: %s", backupParent)
		if logFileHandle != nil {
			logFileHandle.Close()
		}
		os.Exit(1)
	}

	// Create backup transformer
	transformer := NewBackupTransformer()

	// Create backup runner
	runner, err := NewBackupRunner(*backupDir, *iosBackup, *verbose, transformer)
	if err != nil {
		errorLog.Printf("Failed to initialize backup runner: %v", err)
		if logFileHandle != nil {
			logFileHandle.Close()
		}
		os.Exit(1)
	}
	
	// Set log file in runner if specified
	if logFileHandle != nil {
		runner.SetLogFile(logFileHandle)
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Printf("Starting iOS backup with media transformation...\n")
	fmt.Printf("Backup directory: %s\n", *backupDir)
	fmt.Printf("ios_backup: %s\n", *iosBackup)
	fmt.Printf("\nMedia transformations enabled:\n")
	fmt.Printf("  - Image formats: HEIC, GIF, PNG, WEBP, JPEG -> JPEG (500px width)\n")
	fmt.Printf("  - Video formats: MP4, MOV, AVI, etc. -> JPEG thumbnail\n")
	fmt.Printf("\nPress Ctrl+C or send SIGTERM to stop\n\n")

	// Run backup in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- runner.Run()
	}()

	// Wait for either completion or shutdown signal
	exitCode := 0
	select {
	case err := <-errChan:
		if err != nil {
			errorLog.Printf("Backup failed: %v", err)
			exitCode = 1
		} else {
			fmt.Println("\nBackup completed successfully")
		}
		runner.Stop()
	case <-sigChan:
		fmt.Println("\nShutting down gracefully...")
		runner.Stop()
		fmt.Println("Shutdown complete")
	}
	
	// Cleanup and exit
	if logFileHandle != nil {
		logFileHandle.Close()
	}
	os.Exit(exitCode)
}

