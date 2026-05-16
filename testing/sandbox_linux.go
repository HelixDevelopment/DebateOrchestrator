//go:build linux

// Linux-specific sandbox plumbing. Real RLIMIT_AS (memory address
// space) and RLIMIT_CPU (cpu seconds) enforcement is applied to the
// child process ONLY — never to the parent — by wrapping the spawn
// in /usr/bin/prlimit (util-linux). prlimit sets the rlimits in
// itself between fork and exec, so the target binary inherits the
// caps without the parent ever changing its own limits.
//
// Falls back to direct invocation (no kernel enforcement) if prlimit
// is unavailable, with limitsEnforced() reporting accurately.

package testing

import (
	"os/exec"
	"syscall"
)

// prepareSandboxAttr stamps the SysProcAttr fields that survive across
// every fork-exec: own process-group so we can kill the whole subtree
// via syscall.Kill(-pgid, …), and Pdeathsig so an orchestrator crash
// also tears down the child.
func prepareSandboxAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// wrapWithRlimits returns argv that, when spawned, applies the
// requested kernel-level rlimits to the target command. On Linux this
// prepends `/usr/bin/prlimit --as=N --cpu=N --` to the original argv.
// Non-positive limits are omitted from the prlimit flags.
//
// Returns (newArgv, enforced). enforced is true when prlimit is
// available AND at least one cap was requested; false otherwise (so
// callers can set TestExecutionResult.LimitsEnforced honestly).
//
// If neither limit was requested OR prlimit is unavailable, the
// original argv is returned unchanged and enforced=false.
func wrapWithRlimits(argv []string, memBytes int64, cpuSeconds int64) (newArgv []string, enforced bool) {
	if memBytes <= 0 && cpuSeconds <= 0 {
		return argv, false
	}
	prlimitPath, err := exec.LookPath("prlimit")
	if err != nil {
		// prlimit not on PATH — degrade gracefully. Production
		// deployments SHOULD install util-linux for real enforcement.
		return argv, false
	}
	wrapped := []string{prlimitPath}
	if memBytes > 0 {
		wrapped = append(wrapped, formatPrlimitFlag("--as", uint64(memBytes)))
	}
	if cpuSeconds > 0 {
		wrapped = append(wrapped, formatPrlimitFlag("--cpu", uint64(cpuSeconds)))
	}
	wrapped = append(wrapped, "--")
	wrapped = append(wrapped, argv...)
	return wrapped, true
}

// formatPrlimitFlag returns "--flag=N" — prlimit expects single-value
// rlimits as "--<resource>=<value>", which sets both soft and hard to
// the same number.
func formatPrlimitFlag(flag string, value uint64) string {
	return flag + "=" + uintToStr(value)
}

// uintToStr renders a uint64 in base-10 without importing strconv at
// the file scope (keeps the import set tight).
func uintToStr(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// limitsAvailable reports whether the host has the toolchain we need
// to install kernel-level rlimits (util-linux's prlimit). Cached at
// package init time so per-Execute calls don't re-lookpath.
var limitsAvailable = func() bool {
	_, err := exec.LookPath("prlimit")
	return err == nil
}()

// limitsEnforced reports whether wrapWithRlimits would actually apply
// kernel-level caps on this build/host. True on linux WITH prlimit
// installed; false otherwise.
func limitsEnforced() bool { return limitsAvailable }

// killProcessGroup sends SIGKILL to the entire process group rooted at
// pid. Best-effort — ignores ESRCH (group already gone).
func killProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
