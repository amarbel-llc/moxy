package main

import (
	"fmt"
	"net"
	"os"
)

// notifyReady tells a supervising systemd that moxy is serving (sd_notify
// READY=1), for a Type=notify unit (#412). NOTIFY_SOCKET names the datagram
// socket; a leading "@" is an abstract-namespace socket. Without it there is
// no supervisor to tell and this does nothing.
func notifyReady() error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return nil
	}
	if socket[0] == '@' {
		socket = "\x00" + socket[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("sd_notify: dialing %s: %w", os.Getenv("NOTIFY_SOCKET"), err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("READY=1")); err != nil {
		return fmt.Errorf("sd_notify: writing READY=1: %w", err)
	}
	return nil
}
