#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_REPO="$HOME/build/nelsonctl-test"
LOG_DIR="$SCRIPT_DIR/e2e-logs"
CHANGE="uppercase-greet"
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
  git checkout -- .
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

# ── 2. Build nelsonctl ───────────────────────────────────────────────────────

info "Building nelsonctl"
cd "$SCRIPT_DIR"
if ! go build -o nelsonctl ./cmd/nelsonctl; then
  fail "Build failed"
  exit 1
fi
ok "Build succeeded"

# ── 3. Run the pipeline ─────────────────────────────────────────────────────

mkdir -p "$LOG_DIR"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
LOG_FILE="$LOG_DIR/${TIMESTAMP}_${CHANGE}.log"

info "Running pipeline: $CHANGE"
info "Log: $LOG_FILE"

ENV_ARGS=()
if $DEBUG; then
  ENV_ARGS+=(NELSONCTL_DEBUG=1)
fi

set +e
(
  cd "$TEST_REPO"
  env "${ENV_ARGS[@]+"${ENV_ARGS[@]}"}" \
    "$SCRIPT_DIR/nelsonctl" --verbose --no-pr "$CHANGE_PATH" \
    2>&1
) | tee "$LOG_FILE"
PIPE_EXIT=${PIPESTATUS[0]}
set -e

# Symlink latest
ln -sf "$(basename "$LOG_FILE")" "$LOG_DIR/latest"

# ── 4. Parse results ────────────────────────────────────────────────────────

echo ""
info "─── Results ───"

PASS=true

# Parse phase results — works for both verbose TUI and debug output
# Verbose format: "phase N: attempts=X passed=Y"
# Debug format:   PhaseStartEvent and ControllerToolCallResultEvent with Approved:true
while IFS= read -r line; do
  phase_num=$(echo "$line" | grep -oP 'phase \K\d+')
  attempts=$(echo "$line" | grep -oP 'attempts=\K\d+')
  passed=$(echo "$line" | grep -oP 'passed=\K\w+')

  if [[ "$passed" == "true" ]]; then
    ok "Phase $phase_num: passed (attempts=$attempts)"
  else
    fail "Phase $phase_num: FAILED (attempts=$attempts)"
    PASS=false
  fi
done < <(grep -E '^phase [0-9]+: attempts=' "$LOG_FILE" || true)

# If no verbose phase lines found, try debug event parsing
if ! grep -qE '^phase [0-9]+: attempts=' "$LOG_FILE" 2>/dev/null; then
  # Count unique phase starts (Number:N)
  phase_starts=$(grep -oP 'PhaseStartEvent \{Number:\K[0-9]+' "$LOG_FILE" | sort -u)
  for pnum in $phase_starts; do
    # Count attempts for this phase
    attempts=$(grep -c "PhaseStartEvent {Number:$pnum" "$LOG_FILE" || true)
    # Check if there was an approve for this phase's review
    if grep -q 'Tool:approve Approved:true' "$LOG_FILE"; then
      ok "Phase $pnum: passed (attempts=$attempts)"
    else
      fail "Phase $pnum: FAILED (attempts=$attempts)"
      PASS=false
    fi
  done
fi

# Parse final review
final_line=$(grep -oP 'final review passed: \K\w+' "$LOG_FILE" || echo "")
if [[ "$final_line" == "true" ]]; then
  ok "Final review: passed"
elif [[ "$final_line" == "false" ]]; then
  fail "Final review: FAILED"
  PASS=false
elif [[ -z "$final_line" ]]; then
  # Try debug format: look for StateEvent State:FinalReview followed by approve
  if grep -q 'State:FinalReview' "$LOG_FILE"; then
    # Check if final review was approved (there should be an approve after FinalReview state)
    # Get lines after FinalReview state
    final_review_line=$(grep -n 'State:FinalReview' "$LOG_FILE" | tail -1 | cut -d: -f1)
    if [[ -n "$final_review_line" ]]; then
      tail_after=$(tail -n +"$final_review_line" "$LOG_FILE")
      if echo "$tail_after" | grep -q 'Tool:approve Approved:true'; then
        ok "Final review: passed"
      else
        fail "Final review: not approved"
        PASS=false
      fi
    fi
  else
    fail "Final review: not reached"
    PASS=false
  fi
fi

# Pipeline process exit — ignore exit code 1 if it was only git push that failed
# (test repos have no remote)
if [[ $PIPE_EXIT -ne 0 ]]; then
  if grep -q 'git push.*failed' "$LOG_FILE" && grep -q 'State:PR' "$LOG_FILE"; then
    ok "Pipeline completed (git push failed as expected — no remote)"
  else
    fail "Pipeline exited with code $PIPE_EXIT"
    PASS=false
  fi
fi

echo ""
if $PASS; then
  ok "E2E PASS"
  exit 0
else
  fail "E2E FAIL"
  exit 1
fi
