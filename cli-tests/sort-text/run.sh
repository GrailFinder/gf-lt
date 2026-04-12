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
LMODEL=${LMODEL:-gemma-4-31B-it-Q4_K_M}
cd /home/grail/projects/plays/goplays/gf-lt
go run . -cli -msg "$TASK" -model "$LMODEL"

echo ""
echo "=== Done ==="
cp "$LOG_FILE" "$SCRIPT_DIR/latest_run.log"
echo "Log file: $LOG_FILE"
