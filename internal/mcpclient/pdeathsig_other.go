//go:build !linux

package mcpclient

import "os/exec"

// There is no parent-death signal off Linux (moxy#527): a child of a moxy that
// was SIGKILLed is only ended by its own stdin EOF handling.
func setParentDeathSignal(*exec.Cmd) {}

func startChild(cmd *exec.Cmd) error { return cmd.Start() }
