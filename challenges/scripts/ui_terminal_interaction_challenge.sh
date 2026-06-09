#!/usr/bin/env bash
# ui_terminal_interaction_challenge.sh — anti-bluff UI Challenge for
# DebateOrchestrator per CONST-035 + CONST-050(B). Cascade per
# CONST-051(A). Drives the debate-orchestration CLI surface
# ($DEBATEORCHESTRATOR_BIN) through --help / --version / invalid-flag
# and asserts no panic / runtime-error / segfault leaks to the user
# and no crash-via-timeout. No binary → honest SKIP-OK (§11.4.3) — the
# debate orchestrator is primarily a service; a terminal entrypoint is
# exercised only when one is provided. Every PASS cites evidence
# (binary path, bogus-flag exit code).

set -uo pipefail
BIN_PATH="${DEBATEORCHESTRATOR_BIN:-}"
TIMEOUT_SEC="${UI_TIMEOUT_SEC:-30}"
USER_HOSTILE=('panic:' 'goroutine [0-9]+ \[running\]:' 'runtime error:' 'segmentation fault' 'fatal error:')

echo "=== DebateOrchestrator UI Terminal-Interaction Challenge ==="
echo "  bin=$BIN_PATH timeout=${TIMEOUT_SEC}s"

if [[ -z "$BIN_PATH" ]] || [[ ! -x "$BIN_PATH" ]]; then
    echo "[1/4] SKIP: DEBATEORCHESTRATOR_BIN unset or non-executable (service has no terminal UI surface configured) — SKIP-OK: #env-binary-missing"
    echo "=== DebateOrchestrator UI Challenge: PASSED (SKIP-OK) ==="
    exit 0
fi
echo "[1/4] Binary present: PASS"

assert_no_panic() {
    local label="$1" body="$2"
    for pat in "${USER_HOSTILE[@]}"; do
        printf '%s' "$body" | grep -qE "$pat" && { echo "  FAIL: $label leaked user-hostile output: $pat"; return 1; }
    done
}

help_out=$(timeout "$TIMEOUT_SEC" "$BIN_PATH" --help 2>&1 || timeout "$TIMEOUT_SEC" "$BIN_PATH" -h 2>&1 || true)
assert_no_panic "--help" "$help_out" || exit 1
[[ -z "$help_out" ]] && { echo "[2/4] FAIL: empty help output"; exit 1; }
echo "[2/4] Help surface: PASS"

ver_out=$(timeout "$TIMEOUT_SEC" "$BIN_PATH" --version 2>&1 || timeout "$TIMEOUT_SEC" "$BIN_PATH" -v 2>&1 || true)
assert_no_panic "--version" "$ver_out" || exit 1
echo "[3/4] Version surface: PASS"

set +e
bogus=$(timeout "$TIMEOUT_SEC" "$BIN_PATH" --this-flag-does-not-exist 2>&1)
bogus_exit=$?
set -e
[[ "$bogus_exit" -ge 124 ]] && { echo "[4/4] FAIL: invalid flag crashed/hung (exit $bogus_exit)"; exit 1; }
assert_no_panic "bogus-flag" "$bogus" || exit 1
echo "[4/4] Invalid-flag handled: PASS (exit $bogus_exit)"

echo
echo "=== DebateOrchestrator UI Challenge: PASSED ==="
echo "  evidence: bin=$BIN_PATH bogus_exit=$bogus_exit"
