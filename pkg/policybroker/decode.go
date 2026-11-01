package policybroker

import (
	"encoding/json"
	"fmt"
)

// Decode parses one wire line. A line without a type is malformed.
func Decode(line []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(line, &m); err != nil {
		return Message{}, fmt.Errorf("policybroker: malformed message: %w", err)
	}
	if m.Type == "" {
		return Message{}, fmt.Errorf("policybroker: message without a type")
	}
	return m, nil
}
