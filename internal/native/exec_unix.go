//go:build unix

package native

import (
	"os/exec"
	"syscall"
	"time"
)

// killGrace bounds how long cmd.Wait() blocks after the context is cancelled
// (or the direct child exits) before Go force-closes the captured I/O pipes
// and SIGKILLs the child. It exists because a moxin's grandchild can inherit
// stdout/stderr and outlive the direct child (or ignore SIGTERM), holding the
// pipe open so Wait() would otherwise block forever — leaving an async job
// wedged "running" with no terminal wakeup (#344/#345). A package var so tests
// can lower it.
var killGrace = 10 * time.Second

// configureProcessGroup makes cmd lead its own process group and arranges for
// a context cancellation to terminate the WHOLE group, not just the direct
// child. exec.CommandContext's default only SIGKILLs the direct child, so a
// deeper process (just -> nix develop -> sandcastle -> bats, or a wedged
// `timeout` leaf) survives and keeps the inherited pipes open. With Setpgid +
// a group SIGTERM in Cancel + WaitDelay, the dispatch reliably unwinds and the
// job reaches a terminal state.
func configureProcessGroup(cmd *exec.Cmd) {
	// Setpgid makes the child the leader of a new process group whose id
	// equals the child pid, so a single signal to the negated pid reaches
	// every descendant that hasn't deliberately left the group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// On ctx cancel/deadline, SIGTERM the whole group. cmd.Process is set
	// by the time Cancel runs (it fires only after Start). A dead process /
	// already-reaped group yields ESRCH, which is harmless.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}

	// After Cancel (or the direct child exits), bound how long Wait blocks on
	// I/O: past killGrace Go closes the captured pipes — unblocking Wait even
	// when a SIGTERM-ignoring grandchild still holds them — and SIGKILLs the
	// child as a backstop.
	cmd.WaitDelay = killGrace
}
