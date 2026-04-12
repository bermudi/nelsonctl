#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_REPO="$HOME/build/nelsonctl-test"
LOG_DIR="$SCRIPT_DIR/e2e-logs"
CHANGE="guestbook-spa"
RESET=true
DEBUG=false

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    --debug)  DEBUG=true; shift ;;
    --keep)   RESET=false; shift ;;
    --change) CHANGE="$2"; shift 2 ;;
    *)        echo "Unknown flag: $1"; exit 2 ;;
  esac
done

BRANCH="change/$CHANGE"
CHANGE_PATH="specs/changes/$CHANGE"

# ── Helpers ──────────────────────────────────────────────────────────────────

info()  { printf '\033[1;34m▸ %s\033[0m\n' "$*"; }
ok()    { printf '\033[1;32m✓ %s\033[0m\n' "$*"; }
fail()  { printf '\033[1;31m✗ %s\033[0m\n' "$*"; }

# ── 1. Reset test repo ──────────────────────────────────────────────────────

if $RESET; then
  info "Resetting test repo: $TEST_REPO"
  cd "$TEST_REPO"
  git checkout main 2>/dev/null
  git branch -D "$BRANCH" 2>/dev/null || true
  git reset --hard fb27c76b592
  git clean -fd

  # Verify tasks.md has unchecked boxes
  if ! grep -q '^\- \[ \]' "$TEST_REPO/$CHANGE_PATH/tasks.md" 2>/dev/null; then
    fail "tasks.md has no unchecked boxes — repo may not be in expected state"
    exit 1
  fi
  ok "Test repo reset ($(grep -c '^\- \[ \]' "$TEST_REPO/$CHANGE_PATH/tasks.md") unchecked tasks)"
else
  info "Skipping reset (--keep)"
fi

# ── 2. Done ────────────────────────────────────────────────────────────────

echo ""
ok "ready to run nelsonctl!"
