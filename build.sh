#!/bin/bash
set -e
set -o pipefail

if [ -e dist ] ; then 
  rm -rf dist/* 2>&1 >/dev/null
else
  mkdir dist 2>&1 >/dev/null
fi

if [ "$1" == "raspi" ] ; then
  echo "Cross-compiling for ARM64"
  export GOOS=linux 
  export GOARCH=arm64
elif [ "$1" == "--help" -o "$1" == "--help" ] ; then
  echo "Usage: $0 [raspi]"
  exit 1
elif [ "$#" != "0" ] ; then
  echo "Unsupported command-line arguments: " "$@"
  exit 1
fi

go build -C src -o ../dist -ldflags="${LINKER_FLAG1}"

echo "Build output can be found in dist/"
