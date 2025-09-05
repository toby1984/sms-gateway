#!/bin/bash
set -e
set -o pipefail

if [ -e dist ] ; then 
  rm -rf dist/* 2>&1 >/dev/null
else
  mkdir dist 2>&1 >/dev/null
fi

while (( "$#" )); do
    case "$1" in
        raspi|--raspi)
            echo "Cross-compiling for ARM64"
            export GOOS=linux 
            export GOARCH=arm64
            ;;
        -h|--help)
            echo "Usage: $0 [raspi|--raspi]"
            echo "  raspi,--raspi: Compile for ARM" 
            exit 1
            ;;
        -*) # Handle any other unknown flags
            echo "Error: Unknown option: $1" >&2
            exit 1
            ;;
        *) # Handle all non-flag arguments (e.g., file names)
            echo "Unrecognized argument: $1"
            exit 1 
            ;;
    esac
 
    shift
done

now=`date -u +'%Y-%m-%d %H:%M:%S%:::z'`
commit=`git rev-parse HEAD`
version=`cat build_version.txt`

echo "Using build version $version from build_version.txt file"

buildTime="'main.buildTimestamp=$now'"
buildVersion="'main.buildVersion=$version'"
gitCommit="'main.gitCommit=$commit'"
LINKER_FLAGS="-X $buildTime -X $gitCommit -X $buildVersion"

echo "Linker flags: $LINKER_FLAGS"
go build -C src -o ../dist -ldflags="${LINKER_FLAGS}"

echo "Build output can be found in dist/"
