# iOS Backup Transformer Makefile
# Note: This Makefile is for macOS/Linux. For Windows, use build.ps1 or build.bat

.PHONY: build clean clean-all run test deps install

# Binary name
BINARY_NAME=iosbackup_manager

# Path to macOS Frameworks folder (relative to iosbackup_manager directory)
MACOS_FRAMEWORKS=../dispute_buddy/macos/Frameworks

# Path to Windows bin folder (relative to iosbackup_manager directory)
WINDOWS_BIN=../dispute_buddy/windows/bin

# Build the application
build:
	go build -o $(BINARY_NAME) .
	@echo "Copying $(BINARY_NAME) to $(MACOS_FRAMEWORKS)/"
	@mkdir -p $(MACOS_FRAMEWORKS)
	@cp $(BINARY_NAME) $(MACOS_FRAMEWORKS)/$(BINARY_NAME)
	@echo "Build complete: $(BINARY_NAME) copied to macOS Frameworks"

# Install dependencies
deps:
	go mod tidy
	go mod download

# Run the application (requires WATCH_DIR to be set)
# Optional: HEIC_CONVERTER, GIF_CONVERTER, FFMPEG, FFPROBE paths
run:
	@if [ -z "$(WATCH_DIR)" ]; then \
		echo "Usage: make run WATCH_DIR=/path/to/watch [HEIC_CONVERTER=path] [GIF_CONVERTER=path] [FFMPEG=path] [FFPROBE=path]"; \
		echo "Example: make run WATCH_DIR=/tmp/ios_backup"; \
		echo "Example: make run WATCH_DIR=/tmp/ios_backup HEIC_CONVERTER=/usr/local/bin/heic-converter"; \
		exit 1; \
	fi
	@FLAGS="-dir $(WATCH_DIR)"; \
	if [ -n "$(HEIC_CONVERTER)" ]; then \
		FLAGS="$$FLAGS -heic-converter $(HEIC_CONVERTER)"; \
	fi; \
	if [ -n "$(GIF_CONVERTER)" ]; then \
		FLAGS="$$FLAGS -gif-converter $(GIF_CONVERTER)"; \
	fi; \
	if [ -n "$(FFMPEG)" ]; then \
		FLAGS="$$FLAGS -ffmpeg $(FFMPEG)"; \
	fi; \
	if [ -n "$(FFPROBE)" ]; then \
		FLAGS="$$FLAGS -ffprobe $(FFPROBE)"; \
	fi; \
	./$(BINARY_NAME) $$FLAGS

# Test the application (requires converter tools to be available)
test:
	@echo "Note: This test requires heic-converter, gif2jpg, ffmpeg, and ffprobe to be in PATH"
	@echo "Creating test directory..."
	@mkdir -p /tmp/ios_backup_test
	@echo "Starting backup transformer in background..."
	@./$(BINARY_NAME) -dir /tmp/ios_backup_test &
	@PID=$$!; \
	echo "Monitor PID: $$PID"; \
	sleep 2; \
	echo "Stopping monitor..."; \
	kill $$PID 2>/dev/null || true; \
	sleep 1; \
	echo "Cleaning up..."; \
	rm -rf /tmp/ios_backup_test

# Clean build artifacts
clean:
	go clean
	rm -f $(BINARY_NAME)
	rm -f *.log
	@echo "Note: Binary in $(MACOS_FRAMEWORKS) not removed (use 'make clean-all' to remove it too)"

# Clean build artifacts including macOS Frameworks copy
clean-all: clean
	rm -f $(MACOS_FRAMEWORKS)/$(BINARY_NAME)
	@echo "Removed binary from macOS Frameworks"

# Install the binary to GOPATH/bin
install: build
	go install .

# Format the code
fmt:
	go fmt ./...

# Vet the code
vet:
	go vet ./...

# Run all checks
check: fmt vet

# Show help
help:
	@echo "Available targets:"
	@echo "  build     - Build the application and copy to macOS Frameworks"
	@echo "  deps      - Install dependencies"
	@echo "  run       - Run the application (requires WATCH_DIR)"
	@echo "  test      - Run a quick test with temporary files"
	@echo "  clean     - Clean build artifacts (keeps Frameworks copy)"
	@echo "  clean-all - Clean build artifacts including Frameworks copy"
	@echo "  install   - Install binary to GOPATH/bin"
	@echo "  fmt       - Format the code"
	@echo "  vet       - Vet the code"
	@echo "  check     - Run fmt and vet"
	@echo "  help      - Show this help"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make run WATCH_DIR=/path/to/ios/backup"
	@echo "  make run WATCH_DIR=/path/to/ios/backup HEIC_CONVERTER=/usr/local/bin/heic-converter"
	@echo ""
	@echo "The application monitors a directory and converts:"
	@echo "  - HEIC images -> JPEG (overwrites original)"
	@echo "  - GIF images -> JPEG (overwrites original)"
	@echo "  - Video files (MP4, MOV, AVI, MPG, WMV, FLV, WebM, MKV) -> JPEG thumbnail (overwrites original)"
	@echo "  - Other files (except SQLite) -> deleted permanently"
	@echo ""
	@echo "Note: 'make build' automatically copies the binary to:"
	@echo "  $(MACOS_FRAMEWORKS)/$(BINARY_NAME)" 