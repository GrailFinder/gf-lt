#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LOG_FILE="$SCRIPT_DIR/${TIMESTAMP}_run.log"

exec > "$LOG_FILE" 2>&1

echo "=== Running teardown ==="
"$SCRIPT_DIR/teardown.sh"

echo ""
echo "=== Running setup ==="
"$SCRIPT_DIR/setup.sh"

echo ""
echo "=== Running task ==="
TASK=$(cat "$SCRIPT_DIR/task.txt")
cd /home/grail/projects/plays/goplays/gf-lt
go run . -cli -msg "$TASK"

echo ""
echo "=== Done ==="
echo "Log file: $LOG_FILE"
