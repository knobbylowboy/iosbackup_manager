# Windows build script for iosbackup_manager
# Builds the Go application and copies it to the Windows bin directory

$ErrorActionPreference = "Stop"

# Binary name
$BINARY_NAME = "iosbackup_manager.exe"

# Path to Windows bin folder (relative to iosbackup_manager directory)
$WINDOWS_BIN = "../dispute_buddy/windows/bin"

Write-Host "Building $BINARY_NAME..." -ForegroundColor Cyan

# Build the Go application
go build -o $BINARY_NAME .

if ($LASTEXITCODE -ne 0) {
    Write-Host "Build failed!" -ForegroundColor Red
    exit 1
}

Write-Host "Copying $BINARY_NAME to $WINDOWS_BIN/" -ForegroundColor Cyan

# Create Windows bin directory if it doesn't exist
if (-not (Test-Path $WINDOWS_BIN)) {
    New-Item -ItemType Directory -Path $WINDOWS_BIN -Force | Out-Null
}

# Copy the binary
Copy-Item $BINARY_NAME "$WINDOWS_BIN/$BINARY_NAME" -Force

Write-Host "Build complete: $BINARY_NAME copied to Windows bin directory" -ForegroundColor Green

