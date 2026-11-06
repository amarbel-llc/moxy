package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// moxy#412: under a Type=notify unit, moxy tells systemd it is ready by
// writing READY=1 to the datagram socket NOTIFY_SOCKET names.
func TestNotifyReadySendsReadyToNotifySocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listening on %s: %v", socketPath, err)
	}
	defer conn.Close()
	t.Setenv("NOTIFY_SOCKET", socketPath)

	if err := notifyReady(); err != nil {
		t.Fatalf("notifyReady: %v", err)
	}
	buf := make([]byte, 64)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("reading the notification: %v", err)
	}
	if got := string(buf[:n]); got != "READY=1" {
		t.Fatalf("notification = %q, want READY=1", got)
	}
}

// Outside systemd (no NOTIFY_SOCKET) there is no one to tell.
func TestNotifyReadyWithoutNotifySocketIsANoOp(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := notifyReady(); err != nil {
		t.Fatalf("notifyReady: %v", err)
	}
}
