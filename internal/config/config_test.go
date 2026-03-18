package config

import "testing"

func TestParseMinimal(t *testing.T) {
	input := `
[servers.echo]
command = "echo"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv, ok := cfg.Servers["echo"]
	if !ok {
		t.Fatal("expected server 'echo'")
	}
	if srv.Command != "echo" {
		t.Errorf("command: got %q", srv.Command)
	}
}

func TestParseEmpty(t *testing.T) {
	cfg, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers != nil {
		t.Errorf("expected nil servers, got %v", cfg.Servers)
	}
}
