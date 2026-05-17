# DebateOrchestrator — AGENTS.md

## INHERITED FROM Helix Constitution

This submodule is consumed by Helix-family projects (HelixCode, HelixAgent,
ATMOSphere). All rules in `constitution/AGENTS.md` (and the
`constitution/Constitution.md` it references) apply unconditionally to
every change landed here. The rules below extend them — they never weaken
any universal clause.

When this file disagrees with the constitution submodule, the constitution
wins.

Canonical reference: <https://github.com/HelixDevelopment/HelixConstitution>

See `CONSTITUTION.md` (this submodule) for the by-reference list of all
cascaded CONST-NNN anchors. See `CLAUDE.md` (this submodule) for the
agent-operator playbook.

---

## Universal anti-bluff covenant (CONST-035 / §11.4)

Every PASS MUST carry positive runtime evidence captured during execution.
Tests AND Challenges bound equally. The bar for shipping is NOT "tests
pass" but **"users can use the feature"** — applied to this submodule's
debate-orchestration surface, every protocol-phase test that emits PASS
MUST prove the phase actually advanced state on a real (or test-harness
captured) topology with real (or contract-bound) LLM responses.

Forbidden vocabulary in tests / status reports / commit messages when
describing causes: `likely`, `probably`, `maybe`, `might`, `possibly`,
`presumably`, `seems`, `appears to`, `guess`, `seemingly`, `apparently`,
`perhaps`, `supposedly`, `conjectured` (CONST-035 / §11.4.6).

Either prove the cause with captured forensic evidence OR explicitly mark
`UNCONFIRMED:` / `UNKNOWN:` / `PENDING_FORENSICS:` with a tracked-task ID.

---

## Force-push discipline (CONST-043 + CONST-061 / §11.4.41)

Any force-push, force-with-lease, history rewrite, or branch deletion of
`main`/`master` on this submodule's remote requires:

**Gate A (CONST-043):** explicit operator approval in-conversation for the
specific operation.

**Gate B (CONST-061 / §11.4.41):** the mechanical 4-step merge-first
pipeline:

1. `git fetch --all --prune --tags` — capture output
2. Integrate every divergent commit (`git log HEAD..<remote>/<branch>`
   non-empty → rebase / merge / operator-confirmed cherry-pick)
3. Audit integrated tree — no conflict markers, no silent drops,
   previously-passing tests still pass, captured-evidence artefacts still
   validate
4. `git push --force-with-lease` — only after step 3 produces clean
   integration evidence

Both gates required. CONST-043 alone authorises a push that loses remote
work; CONST-061 alone risks pushing without operator awareness.

---

## Decoupling (CONST-051(B))

NEVER inject consuming-project context into this submodule:

- NO hardcoded paths like `/path/to/HelixCode/...`
- NO hardcoded hostnames or remote URLs of consuming projects
- NO consuming-project asset names embedded in source
- NO test fixtures that assume a specific consuming-project layout

When parent-project info is required (e.g., LLM endpoint, model ID, agent
roster), accept it via constructor parameter, env var, or config file.

---

## Test-type coverage (CONST-050(B))

This submodule's coverage matrix:

| Test type | Layer | Mocks permitted |
|---|---|---|
| Unit | `*_test.go` no integration tag | YES |
| Integration | `*_test.go` with `integration` build tag | NO |
| E2E | `comprehensive/StreamDebate_*_test.go` | NO |
| Performance | `testing/perf_*_test.go` | NO |
| Adversarial | `protocol/adversarial_*_test.go` | NO |

The four real-system test types (integration / E2E / performance /
adversarial) MUST exercise real LLM endpoints OR contract-bound test
transport stubs per `testing/transport_*` (per CONST-050(A)).

---

## File-layout discipline (CONST-052)

Lowercase snake_case for all new directories, submodules, files. Existing
Go-convention names (mixedCase function/type names INSIDE `.go` files)
preserved per CONST-052 common-sense exception.
