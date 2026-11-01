// Package policybroker is the client side of the moxy policy-broker protocol
// (RFC 0002, moxy#491): a standing connection to a broker the host names,
// over which moxy asks one verdict (allow, ask, deny) per resource a tool call
// names. The static floor applies only when the client has no answer.
//
// Stability: pre-1.0, with pkg/moxy.
package policybroker

import "encoding/json"

// Version is the protocol version this package speaks.
const Version = 1

// MaxLine bounds one message on the wire (RFC 0002 section 2).
const MaxLine = 64 << 10

// EnvAddr names the variable that carries the broker address (RFC 0002
// section 1): an absolute path is a unix socket, anything else host:port.
const EnvAddr = "CLOWN_POLICY_BROKER"

// EnvSessionKey is the variable spinclass exports into every session it
// launches. When set, FromEnv takes it as the one subject this client serves,
// so a broker answering for another session is refused; unset, the client
// adopts the key in the broker's hello (RFC 0002 section 3).
const EnvSessionKey = "SPINCLASS_SESSION_ID"

// Message types.
const (
	TypeHello   = "hello"
	TypeCheck   = "check"
	TypeVerdict = "verdict"
	TypeError   = "error"
)

// Verdict strings.
const (
	VerdictAllow = "allow"
	VerdictAsk   = "ask"
	VerdictDeny  = "deny"
)

// CapCheck is the one capability defined: the broker answers checks.
const CapCheck = "check"

// Message is the union of the wire messages; each type uses a subset of the
// fields, and unknown fields are ignored on decode (RFC 0002 section 2).
type Message struct {
	V    int    `json:"v"`
	Type string `json:"type"`

	// hello
	Client       string   `json:"client,omitempty"`
	Broker       string   `json:"broker,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Subjects     []string `json:"subjects,omitempty"`
	SessionKey   string   `json:"session_key,omitempty"`

	// check / verdict / error
	ID         int64         `json:"id,omitempty"`
	Subject    string        `json:"subject,omitempty"`
	Op         string        `json:"op,omitempty"`
	Resource   *ResourceWire `json:"resource,omitempty"`
	DeadlineMS int64         `json:"deadline_ms,omitempty"`
	Verdict    string        `json:"verdict,omitempty"`
	Reason     string        `json:"reason,omitempty"`
	Code       string        `json:"code,omitempty"`
	Message    string        `json:"message,omitempty"`
}

// ResourceWire is a resource on the wire.
type ResourceWire struct {
	Kind string `json:"kind"`
	URI  string `json:"uri"`
}

// Encode renders m as one line, newline included.
func Encode(m Message) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
