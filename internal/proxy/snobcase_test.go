package proxy

import "testing"

func TestToSnobCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"execute-command", "execute_command"},
		{"status", "status"},
		{"resource-read", "resource_read"},
		{"a-b-c", "a_b_c"},
		{"already_snake", "already_snake"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := toSnobCase(tt.input); got != tt.want {
				t.Errorf("toSnobCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFromSnobCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"execute_command", "execute-command"},
		{"status", "status"},
		{"resource_read", "resource-read"},
		{"a_b_c", "a-b-c"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := fromSnobCase(tt.input); got != tt.want {
				t.Errorf("fromSnobCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
