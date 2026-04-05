package main

import "testing"

func TestCheckPermission_NoRules(t *testing.T) {
	// nil config permits everything.
	var pc *PermissionConfig
	if err := pc.CheckPermission("/any/path"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	// Empty config permits everything.
	pc = &PermissionConfig{}
	if err := pc.CheckPermission("/any/path"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckPermission_AllowMatch(t *testing.T) {
	pc := &PermissionConfig{
		Allow: []PathRule{{Path: []string{"/home/user/projects"}}},
	}
	if err := pc.CheckPermission("/home/user/projects/foo.go"); err != nil {
		t.Fatalf("expected allowed, got %v", err)
	}
}

func TestCheckPermission_AllowDeniesUnmatched(t *testing.T) {
	pc := &PermissionConfig{
		Allow: []PathRule{{Path: []string{"/home/user/projects"}}},
	}
	if err := pc.CheckPermission("/etc/passwd"); err == nil {
		t.Fatal("expected deny, got nil")
	}
}

func TestCheckPermission_DenyBlocks(t *testing.T) {
	pc := &PermissionConfig{
		Deny: []PathRule{{Path: []string{"/etc"}}},
	}
	if err := pc.CheckPermission("/etc/passwd"); err == nil {
		t.Fatal("expected deny, got nil")
	}
}

func TestCheckPermission_DenyWinsOverAllow(t *testing.T) {
	pc := &PermissionConfig{
		Allow: []PathRule{{Path: []string{"/home"}}},
		Deny:  []PathRule{{Path: []string{"/home/user/.ssh"}}},
	}
	// Allowed path works.
	if err := pc.CheckPermission("/home/user/code/main.go"); err != nil {
		t.Fatalf("expected allowed, got %v", err)
	}
	// Denied path blocked even though allow matches.
	if err := pc.CheckPermission("/home/user/.ssh/id_rsa"); err == nil {
		t.Fatal("expected deny for .ssh, got nil")
	}
}

func TestCheckPermission_DirectoryMatch(t *testing.T) {
	pc := &PermissionConfig{
		Allow: []PathRule{{Path: []string{"/home/user/projects"}}},
	}
	// Files under directory match.
	if err := pc.CheckPermission("/home/user/projects/src/main.go"); err != nil {
		t.Fatalf("expected allowed, got %v", err)
	}
	// The directory itself matches.
	if err := pc.CheckPermission("/home/user/projects"); err != nil {
		t.Fatalf("expected allowed for dir itself, got %v", err)
	}
}

func TestCheckPermission_GlobPattern(t *testing.T) {
	pc := &PermissionConfig{
		Deny: []PathRule{{Path: []string{"/home/user/*.env"}}},
	}
	if err := pc.CheckPermission("/home/user/.env"); err == nil {
		t.Fatal("expected deny for .env, got nil")
	}
	if err := pc.CheckPermission("/home/user/config.toml"); err != nil {
		t.Fatalf("expected allowed, got %v", err)
	}
}
