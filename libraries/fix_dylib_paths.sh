#!/bin/bash
# Fix dylib paths in all executables and libraries

cd "$(dirname "$0")"

echo "Fixing dylib paths in libraries folder..."

# List of libraries that need path updates
LIBS=(
    "libimobiledevice-1.0.6.dylib"
    "libssl.3.dylib"
    "libcrypto.3.dylib"
    "libusbmuxd-2.0.7.dylib"
    "libimobiledevice-glue-1.0.0.dylib"
    "libplist-2.0.4.dylib"
    "libimobiledevice-1.0.dylib"
)

# List of executables that need path updates
EXECUTABLES=(
    "ios_backup"
    "idevice_id"
    "ideviceinfo"
    "ffmpeg"
    "ffprobe"
    "heic-converter"
)

# Fix executables
for exe in "${EXECUTABLES[@]}"; do
    if [ -f "$exe" ]; then
        echo "Fixing $exe..."
        for lib in "${LIBS[@]}"; do
            # Change @executable_path/../Frameworks/ to @executable_path/
            install_name_tool -change "@executable_path/../Frameworks/$lib" "@executable_path/$lib" "$exe" 2>/dev/null
            # Change @rpath/ to @executable_path/
            install_name_tool -change "@rpath/$lib" "@executable_path/$lib" "$exe" 2>/dev/null
        done
    fi
done

# Fix dylibs that reference other dylibs
for dylib in "${LIBS[@]}"; do
    if [ -f "$dylib" ]; then
        echo "Fixing $dylib..."
        # Fix the install name (id) of the dylib itself
        install_name_tool -id "@executable_path/$dylib" "$dylib" 2>/dev/null
        
        # Fix references to other dylibs
        for lib in "${LIBS[@]}"; do
            # Change @executable_path/../Frameworks/ to @executable_path/
            install_name_tool -change "@executable_path/../Frameworks/$lib" "@executable_path/$lib" "$dylib" 2>/dev/null
            # Change @rpath/ to @executable_path/
            install_name_tool -change "@rpath/$lib" "@executable_path/$lib" "$dylib" 2>/dev/null
        done
    fi
done

echo "Re-signing executables..."
for exe in "${EXECUTABLES[@]}"; do
    if [ -f "$exe" ]; then
        codesign -s - --force --deep "$exe" 2>/dev/null
    fi
done

echo "Removing quarantine attributes..."
xattr -cr . 2>/dev/null

echo "Done! Verifying ios_backup..."
otool -L ios_backup | grep -E "imobiledevice|ssl|crypto|usbmuxd|glue|plist"
