package main

import "testing"

func TestParseGodocURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		pkg     string
		symbol  string
		src     bool
		wantErr bool
	}{
		{
			name: "simple package",
			uri:  "godoc://packages/fmt",
			pkg:  "fmt",
		},
		{
			name:   "package with symbol",
			uri:    "godoc://packages/fmt/Println",
			pkg:    "fmt",
			symbol: "Println",
		},
		{
			name:   "package with symbol and src",
			uri:    "godoc://packages/fmt/Println/src",
			pkg:    "fmt",
			symbol: "Println",
			src:    true,
		},
		{
			name: "multi-segment package",
			uri:  "godoc://packages/encoding/json",
			pkg:  "encoding/json",
		},
		{
			name:   "multi-segment package with symbol",
			uri:    "godoc://packages/encoding/json/Decoder",
			pkg:    "encoding/json",
			symbol: "Decoder",
		},
		{
			name:   "multi-segment package with symbol and src",
			uri:    "godoc://packages/encoding/json/Decoder/src",
			pkg:    "encoding/json",
			symbol: "Decoder",
			src:    true,
		},
		{
			name: "deep package path",
			uri:  "godoc://packages/github.com/amarbel-llc/moxy/internal/config",
			pkg:  "github.com/amarbel-llc/moxy/internal/config",
		},
		{
			name:   "deep package path with symbol",
			uri:    "godoc://packages/github.com/amarbel-llc/moxy/internal/config/ServerConfig",
			pkg:    "github.com/amarbel-llc/moxy/internal/config",
			symbol: "ServerConfig",
		},
		{
			name:    "empty URI",
			uri:     "godoc://packages/",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "man://ls",
			wantErr: true,
		},
		{
			name:    "src without symbol",
			uri:     "godoc://packages/fmt/src",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, symbol, src, err := parseGodocURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got pkg=%q symbol=%q src=%v", pkg, symbol, src)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pkg != tt.pkg {
				t.Errorf("pkg = %q, want %q", pkg, tt.pkg)
			}
			if symbol != tt.symbol {
				t.Errorf("symbol = %q, want %q", symbol, tt.symbol)
			}
			if src != tt.src {
				t.Errorf("src = %v, want %v", src, tt.src)
			}
		})
	}
}
