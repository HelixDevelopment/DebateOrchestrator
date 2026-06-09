//go:build !linux

// Non-Linux sandbox plumbing. We honestly execute the child via
// os/exec so the executor still works on macOS / *BSD / Windows for
// development purposes.
//
// Process-GROUP teardown IS implemented here: macOS and the BSDs
// support POSIX process groups (setpgid(2) + kill(-pgid, …)), so the
// child is spawned as its own process-group leader and killProcessGroup
// SIGKILLs the entire group — tearing down any grandchildren (e.g. a
// `sleep 30` spawned by the script) when ctx is cancelled or the
// executor timeout fires. This mirrors sandbox_linux.go's pgrp kill.
//
// What is NOT available off Linux is kernel-level RLIMIT_AS / RLIMIT_CPU
// enforcement via util-linux prlimit. wrapWithRlimits is therefore a
// no-op and limitsEnforced() reports false so callers can populate
// TestExecutionResult.LimitsEnforced honestly; production deployments
// MUST run on Linux for real rlimit sandboxing per the package contract.
// (XNU does not enforce RLIMIT_AS for unprivileged processes — a real
// kernel gap, distinct from the process-group teardown which the
// platform DOES support.)

package testing

import (
	"os/exec"
	"syscall"
)

// prepareSandboxAttr stamps the SysProcAttr fields that make the child
// its own process-group leader (Setpgid). syscall.Setpgid is supported
// on macOS / *BSD, so killProcessGroup can later SIGKILL the whole
// subtree via kill(-pgid, …). Pdeathsig is Linux-only and intentionally
// not set here.
func prepareSandboxAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

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

// killProcessGroup sends SIGKILL to the entire process group rooted at
// pid (the child was made a group leader by prepareSandboxAttr, so its
// pgid == pid). This tears down the child AND every descendant it
// spawned. Best-effort — ignores ESRCH (group already gone). Mirrors
// sandbox_linux.go's killProcessGroup; the negative-pid form is the
// POSIX "signal the whole group" convention, supported on macOS/*BSD.
func killProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
