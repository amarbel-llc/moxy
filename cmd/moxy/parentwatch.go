package main

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// parentPollInterval is how often serve-http checks that the process that
// started it is still its parent. One second matches clown-stdio-bridge's
// watchParent.
const parentPollInterval = time.Second

// watchParent cancels the returned context when the process's parent changes,
// and reports why through the returned func (nil while the parent is still
// the one it started under).
//
// A clown that dies without unwinding (SIGKILL, OOM, a hangup before its
// handler is set) reparents moxy to pid 1 or a subreaper; without this check
// moxy and its child servers then run forever (moxy#526). Cancelling the
// returned context drives the same shutdown a SIGTERM does, so children get
// their stop sequence.
//
// initialPPID <= 1 means moxy was started as pid 1's child on purpose (a
// systemd unit such as krone's mcp-origin): nothing to watch, and the
// returned context is ctx itself.
func watchParent(ctx context.Context, initialPPID int, interval time.Duration, getppid func() int) (context.Context, func() error) {
	if initialPPID <= 1 {
		return ctx, func() error { return nil }
	}
	ctx, cancel := context.WithCancel(ctx)
	var gone atomic.Pointer[error]
	go func() {
		defer cancel()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if now := getppid(); now != initialPPID {
					err := fmt.Errorf("parent process %d is gone (now %d): shutting down", initialPPID, now)
					gone.Store(&err)
					fmt.Fprintf(os.Stderr, "moxy: %v\n", err)
					return
				}
			}
		}
	}()
	return ctx, func() error {
		if p := gone.Load(); p != nil {
			return *p
		}
		return nil
	}
}
