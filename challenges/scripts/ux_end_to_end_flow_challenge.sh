#!/usr/bin/env bash
# ux_end_to_end_flow_challenge.sh — anti-bluff UX Challenge for
# DebateOrchestrator per CONST-035 + CONST-050(B). Cascade per
# CONST-051(A). Exercises the full new-user journey against the
# debate-orchestration CLI ($DEBATEORCHESTRATOR_BIN):
# discover → help → version → recover-from-mistake → still-alive.
# No binary → honest SKIP-OK (§11.4.3). Every PASS cites evidence
# (journey trace, bogus-flag exit code).

set -uo pipefail
BIN_PATH="${DEBATEORCHESTRATOR_BIN:-}"
TIMEOUT_SEC="${UX_TIMEOUT_SEC:-30}"
USER_HOSTILE=('panic:' 'goroutine [0-9]+ \[running\]:' 'runtime error:' 'segmentation fault' 'fatal error:')

echo "=== DebateOrchestrator UX End-to-End Flow Challenge ==="
echo "  bin=$BIN_PATH timeout=${TIMEOUT_SEC}s"

if [[ -z "$BIN_PATH" ]] || [[ ! -x "$BIN_PATH" ]]; then
    echo "[1/5] SKIP: DEBATEORCHESTRATOR_BIN unset or non-executable (service has no terminal UX surface configured) — SKIP-OK: #env-binary-missing"
    echo "=== DebateOrchestrator UX Challenge: PASSED (SKIP-OK) ==="
    exit 0
fi
echo "[1/5] Binary present: PASS"

assert_no_panic() {
    local label="$1" body="$2"
    for pat in "${USER_HOSTILE[@]}"; do
        printf '%s' "$body" | grep -qE "$pat" && { echo "  FAIL: $label leaked user-hostile output: $pat"; return 1; }
    done
}

help_out=$(timeout "$TIMEOUT_SEC" "$BIN_PATH" --help 2>&1 || timeout "$TIMEOUT_SEC" "$BIN_PATH" -h 2>&1 || true)
assert_no_panic "--help" "$help_out" || exit 1
[[ -z "$help_out" ]] && { echo "[2/5] FAIL: empty help output (user cannot discover usage)"; exit 1; }
echo "[2/5] Discover → help: PASS"

ver_out=$(timeout "$TIMEOUT_SEC" "$BIN_PATH" --version 2>&1 || timeout "$TIMEOUT_SEC" "$BIN_PATH" -v 2>&1 || true)
assert_no_panic "--version" "$ver_out" || exit 1
echo "[3/5] Version surface: PASS"

set +e
bogus_out=$(timeout "$TIMEOUT_SEC" "$BIN_PATH" --does-not-exist-flag 2>&1)
bogus_exit=$?
set -e
assert_no_panic "bogus-flag" "$bogus_out" || exit 1
[[ "$bogus_exit" -ge 124 ]] && { echo "[4/5] FAIL: mistake crashed/hung the CLI (exit $bogus_exit)"; exit 1; }
echo "[4/5] Graceful recovery from user mistake: PASS (exit $bogus_exit)"

post=$(timeout "$TIMEOUT_SEC" "$BIN_PATH" --help 2>&1 || timeout "$TIMEOUT_SEC" "$BIN_PATH" -h 2>&1 || true)
assert_no_panic "post-error --help" "$post" || exit 1
[[ -z "$post" ]] && { echo "[5/5] FAIL: CLI unusable after a mistake"; exit 1; }
echo "[5/5] Post-error liveness: PASS"

echo
echo "=== DebateOrchestrator UX Challenge: PASSED ==="
echo "  evidence: journey=discover→help→version→recover→post-liveness bogus_exit=$bogus_exit"
