#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_FILE=$(ls -t "$SCRIPT_DIR"/*_run.log 2>/dev/null | head -1)

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

# Check has-animals directory exists
if [ -d "/tmp/sort-img/has-animals" ]; then
    log_pass "has-animals directory exists"
else
    log_fail "has-animals directory missing"
fi

# Check no-animals directory exists
if [ -d "/tmp/sort-img/no-animals" ]; then
    log_pass "no-animals directory exists"
else
    log_fail "no-animals directory missing"
fi

# Check has-animals contains at least one image
HAS_ANIMALS_FILES=$(ls -1 /tmp/sort-img/has-animals 2>/dev/null | wc -l)
if [ "$HAS_ANIMALS_FILES" -gt 0 ]; then
    log_pass "has-animals contains images ($HAS_ANIMALS_FILES files)"
else
    log_fail "has-animals is empty"
fi

# Check no-animals contains at least one image
NO_ANIMALS_FILES=$(ls -1 /tmp/sort-img/no-animals 2>/dev/null | wc -l)
if [ "$NO_ANIMALS_FILES" -gt 0 ]; then
    log_pass "no-animals contains images ($NO_ANIMALS_FILES files)"
else
    log_fail "no-animals is empty"
fi

# Check total files sorted correctly (3 original files should be in subdirs)
TOTAL_SORTED=$((HAS_ANIMALS_FILES + NO_ANIMALS_FILES))
if [ "$TOTAL_SORTED" -eq 3 ]; then
    log_pass "all 3 files sorted into subdirectories"
else
    log_fail "expected 3 files sorted, got $TOTAL_SORTED"
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
