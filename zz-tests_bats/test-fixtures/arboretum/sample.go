package outline

import (
	"fmt"
	"strings"
)

// Greeter wraps a salutation.
type Greeter struct {
	Salutation string
	count      int
}

// Greet returns the greeting for name.
func (g *Greeter) Greet(name string) string {
	g.count++
	return fmt.Sprintf("%s, %s!", g.Salutation, strings.ToUpper(name))
}

func (g *Greeter) Count() int { return g.count }

func NewGreeter(s string) *Greeter {
	return &Greeter{Salutation: s}
}

const DefaultSalutation = "Hello"

var globalGreeter = NewGreeter(DefaultSalutation)

type Renamer interface {
	Rename(old, new string) error
}
