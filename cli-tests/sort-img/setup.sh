#!/bin/sh

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

mkdir -p /tmp/sort-img

cp "$SCRIPT_DIR/../../assets/ex01.png"  /tmp/sort-img/file1.png
cp "$SCRIPT_DIR/../../assets/helppage.png"  /tmp/sort-img/file2.png
cp "$SCRIPT_DIR/../../assets/yt_thumb.jpg"  /tmp/sort-img/file3.jpg
