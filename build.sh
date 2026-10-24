#!/bin/sh
# Builds livery.exe and the installer. Needs Go, mingw-w64 binutils (windres)
# and NSIS (makensis); on Debian/Ubuntu: apt install binutils-mingw-w64-x86-64 nsis
set -e
cd "$(dirname "$0")"
VERSION=$(sed -n 's/^const version = "\(.*\)"/\1/p' main.go)
MODFILE=${MODFILE:-go.mod}
mkdir -p build
x86_64-w64-mingw32-windres --preprocessor=installer/rc-cat.sh -O coff -o rsrc_windows_amd64.syso installer/livery.rc
GOOS=windows GOARCH=amd64 go build -modfile="$MODFILE" -trimpath -ldflags "-s -w -H windowsgui" -o build/livery.exe .
go test -modfile="$MODFILE" ./...
makensis -V2 -DVERSION="$VERSION" installer/livery.nsi
ls -la build/livery.exe "build/Livery-Setup-$VERSION.exe"
