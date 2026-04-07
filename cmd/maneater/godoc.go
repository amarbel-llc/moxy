package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"unicode"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

const godocPrefix = "godoc://packages/"

var godocTemplates = []protocol.ResourceTemplate{
	{
		URITemplate: "godoc://packages/{package}",
		Name:        "Go package overview",
		Description: "Package summary and exported symbol index from go doc",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "godoc://packages/{package}/{symbol}",
		Name:        "Go symbol documentation",
		Description: "Documentation for an exported Go symbol (type, function, method, constant)",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "godoc://packages/{package}/{symbol}/src",
		Name:        "Go symbol source code",
		Description: "Source code for an exported Go symbol",
		MimeType:    "text/plain",
	},
}

var godocTemplatesV1 = []protocol.ResourceTemplateV1{
	{
		URITemplate: "godoc://packages/{package}",
		Name:        "Go package overview",
		Description: "Package summary and exported symbol index from go doc",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "godoc://packages/{package}/{symbol}",
		Name:        "Go symbol documentation",
		Description: "Documentation for an exported Go symbol (type, function, method, constant)",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "godoc://packages/{package}/{symbol}/src",
		Name:        "Go symbol source code",
		Description: "Source code for an exported Go symbol",
		MimeType:    "text/plain",
	},
}

// parseGodocURI parses a godoc:// URI into package path, symbol name, and
// whether source code was requested. Package paths can contain slashes
// (e.g., encoding/json). Symbol names are distinguished from package path
// segments by starting with an uppercase letter, matching Go's exported
// identifier convention.
func parseGodocURI(uri string) (pkg, symbol string, src bool, err error) {
	if !strings.HasPrefix(uri, godocPrefix) {
		return "", "", false, fmt.Errorf("not a godoc URI: %s", uri)
	}

	path := strings.TrimPrefix(uri, godocPrefix)
	if path == "" {
		return "", "", false, fmt.Errorf("empty godoc URI")
	}

	// Check for trailing /src suffix.
	if strings.HasSuffix(path, "/src") {
		src = true
		path = strings.TrimSuffix(path, "/src")
	}

	// Split into segments. The last segment is a symbol if it starts with
	// an uppercase letter; everything before it is the package path.
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if len(last) > 0 && unicode.IsUpper(rune(last[0])) {
		symbol = last
		parts = parts[:len(parts)-1]
	}

	pkg = strings.Join(parts, "/")
	if pkg == "" {
		return "", "", false, fmt.Errorf("empty package path in godoc URI: %s", uri)
	}

	if src && symbol == "" {
		return "", "", false, fmt.Errorf("godoc /src requires a symbol: %s", uri)
	}

	return pkg, symbol, src, nil
}

// handleGodocRead runs go doc and returns the output.
func handleGodocRead(pkg, symbol string, src bool) (string, error) {
	var args []string
	if src {
		args = append(args, "-src")
	}

	if symbol != "" {
		args = append(args, pkg+"."+symbol)
	} else {
		args = append(args, pkg)
	}

	return runGoDoc(args...)
}

// runGoDoc executes go doc with the given arguments and returns stdout.
func runGoDoc(args ...string) (string, error) {
	cmd := exec.Command("go", append([]string{"doc"}, args...)...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("go doc: %s", errMsg)
		}
		return "", fmt.Errorf("go doc: %w", err)
	}

	return stdout.String(), nil
}
