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
LMODEL=${LMODEL:-Qwen3.5-9B-Q6_K}
go run . -cli -msg "$TASK" -model "$LMODEL"

echo ""
echo "=== Done ==="
cp "$LOG_FILE" "$SCRIPT_DIR/latest_run.log"
echo "Log file: $LOG_FILE"
