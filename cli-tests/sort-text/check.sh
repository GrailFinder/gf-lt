#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_FILE="$SCRIPT_DIR/run.log"

PASS=0
FAIL=0

log_pass() {
    echo "[PASS] $1"
    PASS=$((PASS + 1))
}

log_fail() {
    echo "[FAIL] $1"
    FAIL=$((FAIL + 1))
}

echo "=== Checking results ==="
echo ""

# Check animals directory exists
if [ -d "/tmp/sort-text/animals" ]; then
    log_pass "animals directory exists"
else
    log_fail "animals directory missing"
fi

# Check colors directory exists
if [ -d "/tmp/sort-text/colors" ]; then
    log_pass "colors directory exists"
else
    log_fail "colors directory missing"
fi

# Check animals contain cat/dog
ANIMALS_FILES=$(ls -1 /tmp/sort-text/animals 2>/dev/null | tr '\n' ' ')
if echo "$ANIMALS_FILES" | grep -q "file1.txt" && echo "$ANIMALS_FILES" | grep -q "file3.txt"; then
    log_pass "animals contains animal files"
else
    log_fail "animals missing animal files (got: $ANIMALS_FILES)"
fi

# Check colors contain red/blue
COLORS_FILES=$(ls -1 /tmp/sort-text/colors 2>/dev/null | tr '\n' ' ')
if echo "$COLORS_FILES" | grep -q "file2.txt" && echo "$COLORS_FILES" | grep -q "file4.txt"; then
    log_pass "colors contains color files"
else
    log_fail "colors missing color files (got: $COLORS_FILES)"
fi

# Verify content
if grep -q "cat" /tmp/sort-text/animals/file1.txt 2>/dev/null; then
    log_pass "file1.txt contains 'cat'"
else
    log_fail "file1.txt missing 'cat'"
fi

if grep -q "dog" /tmp/sort-text/animals/file3.txt 2>/dev/null; then
    log_pass "file3.txt contains 'dog'"
else
    log_fail "file3.txt missing 'dog'"
fi

if grep -q "red" /tmp/sort-text/colors/file2.txt 2>/dev/null; then
    log_pass "file2.txt contains 'red'"
else
    log_fail "file2.txt missing 'red'"
fi

if grep -q "blue" /tmp/sort-text/colors/file4.txt 2>/dev/null; then
    log_pass "file4.txt contains 'blue'"
else
    log_fail "file4.txt missing 'blue'"
fi

echo ""
echo "=== Summary ==="
echo "PASSED: $PASS"
echo "FAILED: $FAIL"

if [ $FAIL -gt 0 ]; then
    echo ""
    echo "Log file: $LOG_FILE"
    exit 1
fi

echo ""
echo "All tests passed!"
exit 0
