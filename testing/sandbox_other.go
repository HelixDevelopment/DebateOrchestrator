//go:build !linux

// Non-Linux sandbox plumbing. We honestly execute the child via
// os/exec so the executor still works on macOS / Windows / *BSD for
// development purposes, but kernel-level RLIMIT_AS / RLIMIT_CPU
// enforcement is unavailable on this build. limitsEnforced() reports
// false so the surfaced TestExecutionResult.LimitsEnforced can flag
// the limitation; production deployments MUST run on Linux for real
// sandboxing per the package contract.

package testing

import "os/exec"

// prepareSandboxAttr is a no-op on non-Linux builds — Pdeathsig and
// the Linux process-group semantics don't apply. The child is still
// spawned via os/exec; ctx cancellation + Cmd.Cancel are the only
// teardown levers available.
func prepareSandboxAttr(cmd *exec.Cmd) { _ = cmd }

// wrapWithRlimits is a no-op on non-Linux builds — there is no
// portable equivalent of util-linux prlimit. Returns the argv
// unchanged and enforced=false so the caller can populate
// TestExecutionResult.LimitsEnforced honestly.
func wrapWithRlimits(argv []string, memBytes int64, cpuSeconds int64) (newArgv []string, enforced bool) {
	_, _ = memBytes, cpuSeconds
	return argv, false
}

// limitsEnforced reports whether kernel-level rlimit enforcement is
// active on this build. Always false off Linux.
func limitsEnforced() bool { return false }

// killProcessGroup is a no-op on non-Linux builds — the orchestrator
// relies on (*os/exec.Cmd).Cancel and ctx cancellation to terminate
// the child. Best-effort cleanup is sufficient for dev workflows.
func killProcessGroup(pid int) { _ = pid }
