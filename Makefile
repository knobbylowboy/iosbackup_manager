# iOS Backup File Monitor Makefile

.PHONY: build clean run test deps install

# Binary name
BINARY_NAME=iosbackup_manager

# Build the application
build:
	go build -o $(BINARY_NAME) .

# Install dependencies
deps:
	go mod tidy
	go mod download

# Run the application (requires WATCH_DIR to be set)
run:
	@if [ -z "$(WATCH_DIR)" ]; then \
		echo "Usage: make run WATCH_DIR=/path/to/watch [OUTPUT_FILE=output.txt] [SCAN_EXISTING=true]"; \
		echo "Example: make run WATCH_DIR=/tmp/ios_backup OUTPUT_FILE=analysis.txt"; \
		echo "Example: make run WATCH_DIR=/tmp/ios_backup SCAN_EXISTING=true"; \
		exit 1; \
	fi
	@FLAGS="-dir $(WATCH_DIR)"; \
	if [ -n "$(OUTPUT_FILE)" ]; then \
		FLAGS="$$FLAGS -output $(OUTPUT_FILE)"; \
	fi; \
	if [ "$(SCAN_EXISTING)" = "true" ]; then \
		FLAGS="$$FLAGS -scan-existing"; \
	fi; \
	./$(BINARY_NAME) $$FLAGS

# Test the application with a temporary directory
test:
	@echo "Creating test directory..."
	@mkdir -p /tmp/ios_backup_test
	@echo "Creating initial test files for existing file scan..."
	@echo "This is an existing test file" > /tmp/ios_backup_test/existing.txt
	@echo '{"existing": "data"}' > /tmp/ios_backup_test/existing.json
	@echo "Starting file monitor in background with existing file scan..."
	@./$(BINARY_NAME) -dir /tmp/ios_backup_test -output test_output.txt -scan-existing &
	@PID=$$!; \
	echo "Monitor PID: $$PID"; \
	sleep 3; \
	echo "Creating new test files..."; \
	echo "This is a new test file" > /tmp/ios_backup_test/new.txt; \
	echo '{"new": "data"}' > /tmp/ios_backup_test/new.json; \
	echo "Waiting for processing..."; \
	sleep 3; \
	echo "Stopping monitor..."; \
	kill $$PID; \
	echo "Test results:"; \
	cat test_output.txt; \
	rm -rf /tmp/ios_backup_test test_output.txt

# Clean build artifacts
clean:
	go clean
	rm -f $(BINARY_NAME)
	rm -f file_analysis.txt
	rm -f *.log

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
	@echo "  build     - Build the application"
	@echo "  deps      - Install dependencies"
	@echo "  run       - Run the application (requires WATCH_DIR)"
	@echo "  test      - Run a quick test with temporary files"
	@echo "  clean     - Clean build artifacts"
	@echo "  install   - Install binary to GOPATH/bin"
	@echo "  fmt       - Format the code"
	@echo "  vet       - Vet the code"
	@echo "  check     - Run fmt and vet"
	@echo "  help      - Show this help"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make run WATCH_DIR=/path/to/ios/backup"
	@echo "  make run WATCH_DIR=/path/to/ios/backup OUTPUT_FILE=analysis.txt"
	@echo "  make run WATCH_DIR=/path/to/ios/backup SCAN_EXISTING=true" 