// Package lifecyclelog writes proxy lifecycle events (child spawn/exit,
// JSON-RPC call tracing, moxin invocations, transport write errors) to a
// single append-only log at <log home>/moxy/lifecycle.log so events from
// multiple subsystems form one timeline. The file is owned by diaglog and is
// written only once the process has enabled diagnostics (the moxy binary
// does in main; an embedder that never calls diaglog.Enable writes nothing).
package lifecyclelog

import "code.linenisgreat.com/moxy/internal/diaglog"

// Log writes a formatted line to lifecycle.log. No-op while diagnostics are
// off or if the log file cannot be opened.
func Log(format string, args ...any) {
	diaglog.Printf("lifecycle", format, args...)
}
