package main

import "fmt"

// exitCodeError is a command's own verdict, carried up to main as an error so
// the process ends in one place (moxy#438) instead of a command calling
// os.Exit from inside RunCLI. It prints nothing: the command already said
// what it had to (status's report, narrows' violations).
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// exitWith returns nil for success and an exitCodeError for any other code.
func exitWith(code int) error {
	if code == 0 {
		return nil
	}
	return exitCodeError{code: code}
}
