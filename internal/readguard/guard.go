// Package readguard opens the files a read tool was asked for, after the
// permission gate, without following a symlink the gate did not judge
// (moxy#490; docs/plans/2026-11-11-490-read-symlink-leaves.md).
//
// The gate judges a path with its parents resolved and its leaf as named. A
// reader that then opens the name follows a leaf swapped for a link, or one
// pointing outside the policy. Open instead refuses symlinks, with one
// allowance: a symlink leaf whose fully resolved target the gate itself
// allows (an ordinary link inside a worktree) is opened at that target.
package readguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"code.linenisgreat.com/moxy/pkg/perms"
	"code.linenisgreat.com/moxy/pkg/policybroker"
)

// brokerDialWait bounds the wait for the broker's hello, as the hook's.
const brokerDialWait = 250 * time.Millisecond

// Decider answers the read-policy question for one canonical path.
type Decider interface {
	Decide(ctx context.Context, path string) (perms.Decision, string)
}

// Gate asks the policy broker when one answers, else the static floor
// (folio-perms read: exit 0 allow, 1 ask, anything else deny). A broker that
// is down or hangs is no answer and falls to the floor, never to allow.
type Gate struct {
	Floor  string
	once   sync.Once
	client *policybroker.Client
}

func (g *Gate) Decide(ctx context.Context, path string) (perms.Decision, string) {
	g.once.Do(func() {
		c := policybroker.FromEnv(os.Getenv, nil, "moxy-readguard")
		if c == nil {
			return
		}
		wait, cancel := context.WithTimeout(ctx, brokerDialWait)
		defer cancel()
		c.WaitConnected(wait)
		g.client = c
	})
	if g.client != nil {
		dec, reason, answered := g.client.Check(ctx, perms.Read, perms.Resource{Kind: perms.KindFile, URI: "file://" + path})
		if answered {
			return dec, reason
		}
	}
	out, err := exec.CommandContext(ctx, g.Floor, "read", path).CombinedOutput()
	reason := strings.TrimSpace(string(out))
	var exit *exec.ExitError
	switch {
	case err == nil:
		return perms.Allow, reason
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return perms.Ask, reason
	}
	return perms.Deny, reason
}

func (g *Gate) Close() {
	if g.client != nil {
		g.client.Close()
	}
}

// Opener opens operands under a Decider.
type Opener struct {
	Gate Decider
	// Cwd resolves relative operands.
	Cwd string
}

// Passthrough reports operands that are not paths to judge: moxy's
// substituted blob pipes and unsubstituted blob URIs.
func Passthrough(operand string) bool {
	return strings.HasPrefix(operand, "/dev/fd/") || strings.HasPrefix(operand, "madder://")
}

// Open returns the regular file the operand names, opened without following
// a symlink the gate did not allow.
func (o Opener) Open(ctx context.Context, operand string) (*os.File, error) {
	return o.open(ctx, operand, true)
}

// Verify applies the same checks and opens nothing the caller keeps: for an
// operand a tool must open itself (a directory, or a path its output names).
// The window between the check and the tool's own open stays open.
func (o Opener) Verify(ctx context.Context, operand string) error {
	f, err := o.open(ctx, operand, false)
	if err != nil {
		return err
	}
	return f.Close()
}

func (o Opener) open(ctx context.Context, operand string, regularOnly bool) (*os.File, error) {
	path := operand
	if !filepath.IsAbs(path) {
		path = filepath.Join(o.Cwd, path)
	}
	path = perms.CanonicalPath(path)
	if dec, reason := o.Gate.Decide(ctx, path); dec == perms.Deny {
		return nil, fmt.Errorf("%s: denied: %s", operand, reason)
	}
	f, err := openNoSymlinks(path)
	if errors.Is(err, syscall.ELOOP) {
		f, err = o.openThroughLink(ctx, operand, path)
	}
	if err != nil {
		return nil, err
	}
	if regularOnly {
		fi, err := f.Stat()
		if err == nil && !fi.Mode().IsRegular() {
			err = errors.New("not a regular file")
		}
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("%s: %w", operand, err)
		}
	}
	return f, nil
}

// openThroughLink is the allow-list: path hit a symlink, so its fully
// resolved target is judged and must be allowed, not merely not denied: an
// ask is outside the policy, and nothing here can ask a user.
func (o Opener) openThroughLink(ctx context.Context, operand, path string) (*os.File, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("%s: refusing to follow a symlink that does not resolve: %w", operand, err)
	}
	dec, reason := o.Gate.Decide(ctx, target)
	if dec != perms.Allow {
		return nil, fmt.Errorf("%s: refusing to follow a symlink to %s, which is outside the read policy (%s)", operand, target, orDecision(reason, dec))
	}
	f, err := openNoSymlinks(target)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%s: refusing: the symlink target %s changed while it was opened", operand, target)
		}
		return nil, fmt.Errorf("%s: %w", operand, err)
	}
	return f, nil
}

func orDecision(reason string, dec perms.Decision) string {
	if reason != "" {
		return reason
	}
	return string(dec)
}
