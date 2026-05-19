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

## CONST-068: Shell-script target-shell-parseability mandate (cascaded from constitution submodule §11.4.67)

> Verbatim user mandate (2026-05-19): *"any issue we spot must be fixed, bash scripts as well if they are broken!"* + *"Make sure that this is mandatory rule!"*

> Verbatim 2026-05-19 operator mandate: *"all existing tests and Challenges do work in anti-bluff manner - they MUST confirm that all tested codebase really works as expected! We had been in position that all tests do execute with success and all Challenges as well, but in reality the most of the features does not work and can't be used! This MUST NOT be the case and execution of tests and Challenges MUST guarantee the quality, the completition and full usability by end users of the product!"*

Every committed shell script MUST be parseable by its target interpreter (`sh -n` for `/bin/sh`, `bash -n` for `/bin/bash`, etc.) AND MUST declare a shebang matching its actual syntax usage. Bash-only constructs (`>(...)`, `<(...)`, `[[ ]]`, `<<<`, arrays, `${var^^}`, etc.) used in scripts that may be invoked via `sh script.sh` MUST be wrapped in `eval` so the parser sees only a string (target shells like mksh parse the entire script before executing — runtime guards cannot save a parse-time rejection). Honest shebangs only: `#!/bin/bash` only if bash actually expected; `#!/bin/sh` requires POSIX-clean body. Fix at source per §11.4.1, never at callsites. Composes with §11.4.1 / §11.4.4 / §11.4.6 / §11.4.50 / §11.4.51. Pre-build gate `CM-SCRIPT-TARGET-SHELL-PARSEABLE` runs `sh -n` on every in-scope script. No escape hatch — no `--skip-parseability-check`, `--bash-only-script`, `--runtime-guard-suffices` flag.

**Cascade requirement:** This anchor (verbatim or by `CONST-068` ID reference) MUST appear in every owned submodule's `CONSTITUTION.md`, `CLAUDE.md`, and `AGENTS.md`. See constitution submodule `Constitution.md` §11.4.67 for the full mandate.
