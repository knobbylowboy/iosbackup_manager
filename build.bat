@echo off
REM Windows batch file build script for iosbackup_manager
REM Builds the Go application and copies it to the Windows bin directory

set BINARY_NAME=iosbackup_manager.exe
set WINDOWS_BIN=..\dispute_buddy\windows\bin

echo Building %BINARY_NAME%...

go build -o %BINARY_NAME% .

if errorlevel 1 (
    echo Build failed!
    exit /b 1
)

echo Copying %BINARY_NAME% to %WINDOWS_BIN%\

REM Create Windows bin directory if it doesn't exist
if not exist "%WINDOWS_BIN%" mkdir "%WINDOWS_BIN%"

REM Copy the binary
copy /Y %BINARY_NAME% "%WINDOWS_BIN%\%BINARY_NAME%"

echo Build complete: %BINARY_NAME% copied to Windows bin directory

