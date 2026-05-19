#!/usr/bin/env bash
# debateorchestrator_describe_challenge.sh
#
# Round-272 paired-mutation deep-doc challenge for digital.vasic.debate.
#
# Validates that:
#   1. The deep-doc ledger (docs/test-coverage.md) lists every exported
#      symbol from debate.go + orchestrator/orchestrator.go +
#      orchestrator/types.go + orchestrator/api.go.
#   2. The multi-locale fixture (tests/fixtures/debateorchestrator/
#      payloads.json) parses and contains at least 5 locales.
#   3. The multi-locale runner (challenges/runner/main.go) builds and
#      runs, byte-preserving non-ASCII debate topics + lesson content
#      through the real orchestrator.Orchestrator + capturingInvoker
#      across ConductDebate, LessonBank Add/Get/Search/Count,
#      APIAdapter CreateDebate, error propagation, concurrency, and
#      rejection paths.
#   4. The README enumerates the round-272 anti-bluff guarantees.
#
# Paired-mutation invariant (CONST-035 + CONST-050(B)):
#   With --anti-bluff-mutate the script plants a deliberate symbol-rename
#   mutation in a tmp copy of the ledger (ConductDebate ->
#   ConductDebate_MUTATED), reruns validation, and asserts the gate
#   FAILS with exit 99. This proves the gate actually catches
#   ledger-vs-source drift instead of rubber-stamping it.
#
# Exit codes:
#   0  -- gate PASS on clean tree
#   1  -- gate FAIL on clean tree (real failure to fix)
#   99 -- paired-mutation correctly detected (good -- proves anti-bluff)
#   2  -- usage / environment error

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

MUTATE=0
for arg in "$@"; do
    case "$arg" in
        --anti-bluff-mutate) MUTATE=1 ;;
        --help|-h)
            sed -n '1,32p' "$0"
            exit 0
            ;;
        *)
            echo "unknown argument: $arg" >&2
            exit 2
            ;;
    esac
done

PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); echo "  FAIL: $1"; }

LEDGER="${MODULE_DIR}/docs/test-coverage.md"
FIXTURE="${MODULE_DIR}/tests/fixtures/debateorchestrator/payloads.json"
RUNNER="${MODULE_DIR}/challenges/runner/main.go"
README="${MODULE_DIR}/README.md"

LEDGER_WORK="${LEDGER}"
TMP_LEDGER=""
if [ "${MUTATE}" -eq 1 ]; then
    TMP_LEDGER="$(mktemp)"
    cp "${LEDGER}" "${TMP_LEDGER}"
    # Plant a rename so the symbol no longer matches what the source declares.
    sed -i 's/ConductDebate/ConductDebate_MUTATED/g' "${TMP_LEDGER}"
    LEDGER_WORK="${TMP_LEDGER}"
    echo "=== DebateOrchestrator Describe Challenge (anti-bluff-mutate mode) ==="
else
    echo "=== DebateOrchestrator Describe Challenge (clean mode) ==="
fi
echo ""

# Section 1: ledger presence and freshness
echo "Section 1: docs/test-coverage.md ledger"
if [ ! -f "${LEDGER_WORK}" ]; then
    fail "ledger missing at ${LEDGER_WORK}"
else
    pass "ledger present"
    if grep -q "round-272" "${LEDGER_WORK}"; then
        pass "ledger marked round-272"
    else
        fail "ledger missing round-272 marker"
    fi
    if grep -q "execution of tests and Challenges MUST guarantee" "${LEDGER_WORK}"; then
        pass "ledger carries Article XI §11.9 mandate"
    else
        fail "ledger missing Article XI §11.9 mandate"
    fi
fi

# Section 2: every exported package symbol appears in ledger.
echo ""
echo "Section 2: structural symbol cross-reference"

EXPECTED_SYMBOLS=(
    # root debate package (debate.go)
    "LessonBank" "Lesson" "LessonBankConfig" "NewLessonBank"
    "DefaultLessonBankConfig"
    # orchestrator package (orchestrator.go + types.go + api.go)
    "Orchestrator" "NewOrchestrator" "NewDebateOrchestrator"
    "OrchestratorConfig" "DefaultOrchestratorConfig"
    "ConductDebate" "RegisterProvider" "GetStatistics" "GetAgentPool"
    "CreateSession" "GetSession" "ListSessions" "CancelSession" "Cleanup"
    "WithProviderInvoker" "ProviderInvoker" "ProviderRegistry"
    "DebateRequest" "DebateResponse" "PhaseResponse" "AgentResponse"
    "ConsensusResponse" "DebateMetrics" "OrchestratorStats"
    "APIAdapter" "NewAPIAdapter" "APICreateDebateRequest"
    "APIParticipantConfig" "EffectiveProvider" "EffectiveModel"
    "APIStatistics"
)

CHECKED=0
MISSING=0
for sym in "${EXPECTED_SYMBOLS[@]}"; do
    CHECKED=$((CHECKED + 1))
    if grep -qE "\\b${sym}\\b" "${LEDGER_WORK}"; then
        : # found
    else
        fail "ledger missing symbol ${sym}"
        MISSING=$((MISSING + 1))
    fi
done
if [ "${MISSING}" -eq 0 ]; then
    pass "all ${CHECKED} structural symbols cross-referenced in ledger"
fi

# Section 3: multi-locale fixture sanity
echo ""
echo "Section 3: multi-locale fixture"
if [ ! -f "${FIXTURE}" ]; then
    fail "fixture missing at ${FIXTURE}"
else
    pass "fixture present"
    LOCALE_COUNT=$(grep -oE '"locale":\s*"[^"]+"' "${FIXTURE}" | sort -u | wc -l)
    if [ "${LOCALE_COUNT}" -ge 5 ]; then
        pass "fixture covers ${LOCALE_COUNT} locales (>=5)"
    else
        fail "fixture covers only ${LOCALE_COUNT} locales (<5)"
    fi
fi

# Section 4: runner builds + runs against every section
echo ""
echo "Section 4: multi-locale runner build + run (real Orchestrator + capturingInvoker)"
if [ ! -f "${RUNNER}" ]; then
    fail "runner missing at ${RUNNER}"
else
    pass "runner source present"
    cd "${MODULE_DIR}"
    if go build -o /tmp/debate_round272_runner ./challenges/runner/ 2>/tmp/debate_build.log; then
        pass "runner builds"
        if /tmp/debate_round272_runner -fixtures "${FIXTURE}" > /tmp/debate_run.log 2>&1; then
            pass "runner exit 0 across every section + locale"
            # Per-locale + per-section PASS coverage
            if grep -q "PASS: \[Section1\]\[NewDebateOrchestrator\]" /tmp/debate_run.log; then
                pass "Section 1 NewDebateOrchestrator default-surface"
            else
                fail "Section 1 NewDebateOrchestrator missing"
            fi
            if grep -q "PASS: \[Section1\]\[CancelSession\]" /tmp/debate_run.log; then
                pass "Section 1 CancelSession lifecycle"
            else
                fail "Section 1 CancelSession missing"
            fi
            if grep -q "PASS: \[Section2\]\[ConductDebate\]\[sr\]" /tmp/debate_run.log; then
                pass "Section 2 Cyrillic (sr) ConductDebate round-trip"
            else
                fail "Section 2 Cyrillic (sr) ConductDebate missing"
            fi
            if grep -q "PASS: \[Section2\]\[ConductDebate\]\[ja\]" /tmp/debate_run.log; then
                pass "Section 2 Japanese (ja) ConductDebate round-trip"
            else
                fail "Section 2 Japanese (ja) ConductDebate missing"
            fi
            if grep -q "PASS: \[Section2\]\[ConductDebate\]\[ar\]" /tmp/debate_run.log; then
                pass "Section 2 Arabic (ar) ConductDebate round-trip"
            else
                fail "Section 2 Arabic (ar) ConductDebate missing"
            fi
            if grep -q "PASS: \[Section2\]\[ConductDebate\]\[zh-CN\]" /tmp/debate_run.log; then
                pass "Section 2 Han (zh-CN) ConductDebate round-trip"
            else
                fail "Section 2 Han (zh-CN) ConductDebate missing"
            fi
            if grep -q "PASS: \[Section3\]\[LessonBank\]\[sr\]" /tmp/debate_run.log; then
                pass "Section 3 LessonBank Cyrillic CRUD+search"
            else
                fail "Section 3 LessonBank sr missing"
            fi
            if grep -q "PASS: \[Section3\]\[LessonBank\]\[ar\]" /tmp/debate_run.log; then
                pass "Section 3 LessonBank Arabic CRUD+search"
            else
                fail "Section 3 LessonBank ar missing"
            fi
            if grep -q "PASS: \[Section4\]\[APIAdapter\]\[ja\]" /tmp/debate_run.log; then
                pass "Section 4 APIAdapter Japanese (EffectiveProvider alias resolution)"
            else
                fail "Section 4 APIAdapter ja missing"
            fi
            if grep -q "PASS: \[Section4\]\[APIAdapter\]\[zh-CN\]" /tmp/debate_run.log; then
                pass "Section 4 APIAdapter Han (EffectiveModel alias resolution)"
            else
                fail "Section 4 APIAdapter zh-CN missing"
            fi
            if grep -q "PASS: \[Section5\]\[invoker-error-marker\]" /tmp/debate_run.log; then
                pass "Section 5 ProviderInvoker error propagation marker"
            else
                fail "Section 5 invoker-error-marker missing"
            fi
            if grep -q "PASS: \[Section6\]\[parallel\]" /tmp/debate_run.log; then
                pass "Section 6 parallel ConductDebate concurrency"
            else
                fail "Section 6 parallel missing"
            fi
            if grep -q "PASS: \[Section7\]\[ConductDebate(nil)\]" /tmp/debate_run.log; then
                pass "Section 7 ConductDebate(nil) rejection"
            else
                fail "Section 7 nil-request rejection missing"
            fi
            if grep -q "PASS: \[Section7\]\[ConductDebate(empty-topic)\]" /tmp/debate_run.log; then
                pass "Section 7 ConductDebate(empty-topic) rejection"
            else
                fail "Section 7 empty-topic rejection missing"
            fi
            if grep -q "PASS: \[Section7\]\[RegisterProvider(neg-score)\]" /tmp/debate_run.log; then
                pass "Section 7 RegisterProvider(neg-score) rejection"
            else
                fail "Section 7 neg-score rejection missing"
            fi
        else
            fail "runner exit non-zero -- see /tmp/debate_run.log"
            sed -n '1,80p' /tmp/debate_run.log
        fi
    else
        fail "runner build failed -- see /tmp/debate_build.log"
        sed -n '1,40p' /tmp/debate_build.log
    fi
    rm -f /tmp/debate_round272_runner
fi

# Section 5: README round-272 anti-bluff section
echo ""
echo "Section 5: README round-272 anti-bluff section"
if grep -q "Anti-bluff guarantees" "${README}"; then
    pass "README declares Anti-bluff guarantees"
else
    fail "README missing Anti-bluff guarantees section"
fi
if grep -q "round-272" "${README}"; then
    pass "README marked round-272"
else
    fail "README missing round-272 marker"
fi

# Cleanup mutated ledger if any
if [ -n "${TMP_LEDGER}" ]; then
    rm -f "${TMP_LEDGER}"
fi

echo ""
echo "=== Summary: ${PASS}/${TOTAL} PASS, ${FAIL} FAIL ==="

if [ "${MUTATE}" -eq 1 ]; then
    if [ "${FAIL}" -gt 0 ]; then
        echo "anti-bluff-mutate: gate correctly detected planted mutation (exit 99)"
        exit 99
    else
        echo "anti-bluff-mutate: gate FAILED to detect planted mutation -- bluff!"
        exit 1
    fi
fi

if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
exit 0
