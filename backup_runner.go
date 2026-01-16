package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// BackupRunner runs ios_backup and processes files as they're reported
type BackupRunner struct {
	backupDir    string
	iosBackup    string
	verbose      bool
	logFile      *os.File       // Optional log file for output
	transformer  *BackupTransformer
	stopChan     chan struct{}
	wg           sync.WaitGroup // Tracks main goroutines
	processingWg sync.WaitGroup // Tracks file processing goroutines
	activeCount  int64          // Number of files currently being processed
	totalCount   int64          // Total number of files processed or being processed
	countMu      sync.Mutex     // Protects queue counters
}

// NewBackupRunner creates a new backup runner that calls ios_backup
func NewBackupRunner(backupDir string, iosBackupPath string, verbose bool, transformer *BackupTransformer) (*BackupRunner, error) {
	runner := &BackupRunner{
		backupDir:   backupDir,
		iosBackup:   iosBackupPath,
		verbose:     verbose,
		transformer: transformer,
		stopChan:    make(chan struct{}),
	}
	
	// Set up queue depth tracking functions in transformer
	transformer.queueDepth = func() (int64, int64) {
		runner.countMu.Lock()
		defer runner.countMu.Unlock()
		return runner.activeCount, runner.totalCount
	}
	transformer.incrementTotal = func() {
		runner.countMu.Lock()
		runner.totalCount++
		runner.countMu.Unlock()
	}
	
	return runner, nil
}

// SetLogFile sets an optional log file for capturing output
func (br *BackupRunner) SetLogFile(logFile *os.File) {
	br.logFile = logFile
}

// processFile processes a saved file reported by ios_backup
// This function includes panic recovery to prevent crashes from malformed files
func (br *BackupRunner) processFile(filePath string, domain string) {
	// Recover from any panics to prevent crash
	defer func() {
		if r := recover(); r != nil {
			errorLog.Printf("PANIC recovered in processFile for %s: %v", filepath.Base(filePath), r)
		}
	}()

	// Skip if file no longer exists
	stat, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		errorLog.Printf("Error stating file %s: %v", filePath, err)
		return
	}

	// Extract file extension from domain (which contains original filename)
	// e.g., domain: "/.b/6/Library/.../IMG_1234.HEIC" -> extension: ".HEIC"
	fileExt := strings.ToLower(filepath.Ext(domain))

	// Create timing info
	timing := &FileTiming{
		CreatedTime:     stat.ModTime(),
		DiscoveredTime:  time.Now(),
		DiscoveryMethod: "ios_backup",
	}

	// Increment active count when starting to process
	br.countMu.Lock()
	br.activeCount++
	br.countMu.Unlock()

	// Process the file with the extension from the domain
	br.transformer.ProcessFileByExtension(filePath, fileExt, timing)

	// Decrement active count when done
	br.countMu.Lock()
	br.activeCount--
	wasLastJob := br.activeCount == 0
	totalProcessed := br.totalCount
	br.countMu.Unlock()

	// Log when all jobs are complete
	if wasLastJob && totalProcessed > 0 {
		infoLog.Printf("All jobs completed. Total files processed: %d", totalProcessed)
	}
}

// parseSavedFileLine parses a FILE_SAVED line from ios_backup stderr
// Format: FILE_SAVED: path=<relative_path> domain=<domain>
// Returns the full file path and domain, or empty strings if not a FILE_SAVED line
func (br *BackupRunner) parseSavedFileLine(line string) (string, string) {
	// Match: FILE_SAVED: path=<path> domain=<domain>
	if !strings.HasPrefix(line, "FILE_SAVED: ") {
		return "", ""
	}

	// DEBUG: Log the raw line (verbose mode only)
	if br.verbose {
		infoLog.Printf("DEBUG: Parsing FILE_SAVED line: %s", line)
	}

	// Use regex to extract path and domain
	re := regexp.MustCompile(`path=([^\s]+)(?:\s+domain=([^\s]+))?`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 2 {
		if br.verbose {
			infoLog.Printf("DEBUG: Regex didn't match. Matches: %v", matches)
		}
		return "", ""
	}

	relativePath := matches[1]
	domain := ""
	if len(matches) > 2 {
		domain = matches[2]
	}

	// DEBUG: Log what was extracted (verbose mode only)
	if br.verbose {
		infoLog.Printf("DEBUG: Extracted - relativePath: %s, domain: %s", relativePath, domain)
	}

	// Convert relative path to full path
	// The relativePath already includes the device ID folder (e.g., 00008110.../Snapshot/...)
	// and backupDir is /path/to/00008110..., so we need to use the parent directory
	backupParent := filepath.Dir(br.backupDir)
	fullPath := filepath.Join(backupParent, relativePath)
	fullPath = filepath.Clean(fullPath)

	if br.verbose {
		infoLog.Printf("DEBUG: Full path: %s", fullPath)
	}

	// Verify file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		if br.verbose {
			errorLog.Printf("DEBUG: File does not exist: %s", fullPath)
		}
		return "", ""
	}

	return fullPath, domain
}

// Run executes ios_backup and processes files as they're reported
func (br *BackupRunner) Run() error {
	// Find ios_backup executable
	iosBackupPath, found := findExecutable(br.iosBackup)
	if !found {
		return fmt.Errorf("ios_backup not found: %s", br.iosBackup)
	}

	// Get parent directory of backup (ios_backup expects parent dir as backup destination)
	backupParent := filepath.Dir(br.backupDir)
	
	// Create context with timeout for the command (24 hours max)
	// This prevents indefinite hangs if ios_backup has issues
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()

	// Build command arguments with domain filters
	args := []string{
		"--domain", "*SMS*",
		"--domain", "*sms*",
		"--domain", "*AddressBook*",
		"--domain", "*WhatsApp*",
		"--domain", "*whatsapp*",
		"--domain", "*ChatStorage.sqlite*",
		"--domain", "*Message/Media/*", // WhatsApp media
		"backup",
		backupParent,
	}

	// Start ios_backup with domain filters
	cmd := exec.CommandContext(ctx, iosBackupPath, args...)
	
	// Set up stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ios_backup: %v", err)
	}

	infoLog.Printf("Started ios_backup backup to: %s", br.backupDir)

	// Process stdout (forward to console and parse for FILE_SAVED lines)
	stdoutErrChan := make(chan error, 1)
	br.wg.Add(1)
	go func() {
		stdoutErrChan <- br.processOutput(stdout, os.Stdout)
	}()

	// Process stderr (forward to console and parse for FILE_SAVED lines)
	stderrErrChan := make(chan error, 1)
	br.wg.Add(1)
	go func() {
		stderrErrChan <- br.processStderr(stderr)
	}()

	// Wait for command to complete
	err = cmd.Wait()
	
	// Wait for output processors to finish
	br.wg.Wait()
	
	// Check for output processing errors
	var outputErrors []string
	if stdoutErr := <-stdoutErrChan; stdoutErr != nil {
		outputErrors = append(outputErrors, fmt.Sprintf("stdout error: %v", stdoutErr))
	}
	if stderrErr := <-stderrErrChan; stderrErr != nil {
		outputErrors = append(outputErrors, fmt.Sprintf("stderr error: %v", stderrErr))
	}
	
	// Wait for all file processing to complete
	br.processingWg.Wait()

	// Report any command errors
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ios_backup timed out after 24 hours")
		}
		return fmt.Errorf("ios_backup failed: %v", err)
	}

	// Report output processing errors as warnings (non-fatal)
	if len(outputErrors) > 0 {
		errorLog.Printf("Warning: Output processing encountered errors: %v", outputErrors)
	}

	infoLog.Printf("ios_backup completed successfully")
	return nil
}

// processOutput processes output from stdout, parsing for FILE_SAVED lines and forwarding to console
func (br *BackupRunner) processOutput(pipe io.Reader, output io.Writer) error {
	defer br.wg.Done()
	
	scanner := bufio.NewScanner(pipe)
	filesSeen := 0
	for scanner.Scan() {
		line := scanner.Text()
		
		// Parse for FILE_SAVED lines (they might be in stdout)
		filePath, domain := br.parseSavedFileLine(line)
		if filePath != "" {
			filesSeen++
			if br.verbose {
				infoLog.Printf("DEBUG: Detected FILE_SAVED #%d in stdout: %s (domain: %s)", filesSeen, filepath.Base(filePath), domain)
			}
			// Process the file asynchronously with panic recovery
			br.processingWg.Add(1)
			go func(fp string, dom string) {
				defer func() {
					if r := recover(); r != nil {
						errorLog.Printf("PANIC recovered in file processing goroutine: %v", r)
					}
					br.processingWg.Done()
				}()
				br.processFile(fp, dom)
			}(filePath, domain)
		}
		
		// Filter out noise unless verbose mode is enabled
		shouldOutput := true
		if !br.verbose {
			// Skip empty lines and whitespace-only lines
			if strings.TrimSpace(line) == "" {
				shouldOutput = false
			} else if strings.HasPrefix(line, "FILE_FILTERED:") || strings.HasPrefix(line, "Receiving domain:") {
				shouldOutput = false
			}
		}
		
		if shouldOutput {
			fmt.Fprintln(output, line)
			// Also write to log file if specified
			if br.logFile != nil {
				fmt.Fprintln(br.logFile, line)
			}
		}
	}

	if filesSeen > 0 {
		infoLog.Printf("Detected %d FILE_SAVED lines in stdout", filesSeen)
	}

	if err := scanner.Err(); err != nil {
		errorLog.Printf("Error reading output: %v", err)
		return fmt.Errorf("scanner error on stdout: %v", err)
	}
	return nil
}

// processStderr processes stderr output, forwarding it and parsing for FILE_SAVED lines
func (br *BackupRunner) processStderr(pipe io.Reader) error {
	defer br.wg.Done()
	
	scanner := bufio.NewScanner(pipe)
	filesSeen := 0
	for scanner.Scan() {
		line := scanner.Text()
		
		// Filter out noise unless verbose mode is enabled
		shouldForward := true
		if !br.verbose {
			// Skip empty lines and whitespace-only lines
			if strings.TrimSpace(line) == "" {
				shouldForward = false
			} else if strings.HasPrefix(line, "FILE_FILTERED:") || strings.HasPrefix(line, "Receiving domain:") {
				shouldForward = false // Skip these lines in non-verbose mode
			}
		}
		
		// Forward the line to stderr (if not filtered)
		if shouldForward {
			fmt.Fprintln(os.Stderr, line)
			// Also write to log file if specified
			if br.logFile != nil {
				fmt.Fprintln(br.logFile, line)
			}
		}
		
		// Parse for FILE_SAVED lines
		filePath, domain := br.parseSavedFileLine(line)
		if filePath != "" {
			filesSeen++
			if br.verbose {
				infoLog.Printf("DEBUG: Detected FILE_SAVED #%d: %s (domain: %s)", filesSeen, filepath.Base(filePath), domain)
			}
			// Process the file asynchronously with panic recovery
			br.processingWg.Add(1)
			go func(fp string, dom string) {
				defer func() {
					if r := recover(); r != nil {
						errorLog.Printf("PANIC recovered in file processing goroutine: %v", r)
					}
					br.processingWg.Done()
				}()
				br.processFile(fp, dom)
			}(filePath, domain)
		}
	}

	if br.verbose {
		if filesSeen > 0 {
			infoLog.Printf("DEBUG: Total FILE_SAVED lines detected: %d", filesSeen)
		} else {
			infoLog.Printf("DEBUG: No FILE_SAVED lines detected in stderr")
		}
	}

	if err := scanner.Err(); err != nil {
		errorLog.Printf("Error reading stderr: %v", err)
		return fmt.Errorf("scanner error on stderr: %v", err)
	}
	return nil
}

// Stop stops the backup runner gracefully
func (br *BackupRunner) Stop() {
	infoLog.Println("Shutdown requested, waiting for all files to be processed...")
	
	// Wait for output processors to finish
	br.wg.Wait()
	
	// Wait for all file processing to complete
	br.processingWg.Wait()
	
	br.countMu.Lock()
	finalTotal := br.totalCount
	br.countMu.Unlock()
	
	infoLog.Printf("Backup runner stopped. Total files processed: %d", finalTotal)
}

