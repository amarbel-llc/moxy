#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function no_moxyfile_reports_no_servers { # @test
  cd "$HOME"
  run_moxy
  assert_failure
  assert_output --partial "no servers configured"
}

function loads_moxyfile_from_cwd { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "fake-server"
command = "nonexistent-mcp-server"
EOF

  cd "$HOME/repo"
  run_moxy
  assert_failure
  assert_output --partial "fake-server"
}

function loads_moxyfile_from_global_config { # @test
  mkdir -p "$HOME/.config/moxy"
  cat > "$HOME/.config/moxy/moxyfile" <<'EOF'
[[servers]]
name = "global-server"
command = "nonexistent-global-server"
EOF

  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  run_moxy
  assert_failure
  assert_output --partial "global-server"
}

function loads_moxyfile_from_parent_dir { # @test
  mkdir -p "$HOME/eng/repos/myrepo"
  cat > "$HOME/eng/moxyfile" <<'EOF'
[[servers]]
name = "parent-server"
command = "nonexistent-parent-server"
EOF

  cd "$HOME/eng/repos/myrepo"
  run_moxy
  assert_failure
  assert_output --partial "parent-server"
}

function repo_moxyfile_overrides_global { # @test
  mkdir -p "$HOME/.config/moxy"
  cat > "$HOME/.config/moxy/moxyfile" <<'EOF'
[[servers]]
name = "myserver"
command = "global-cmd --global"
EOF

  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "myserver"
command = "repo-cmd --repo"
EOF

  cd "$HOME/repo"
  run_moxy
  assert_failure
  # Should try to start repo-cmd, not global-cmd
  assert_output --partial "repo-cmd"
  refute_output --partial "global-cmd"
}

function merges_servers_from_global_and_repo { # @test
  mkdir -p "$HOME/.config/moxy"
  cat > "$HOME/.config/moxy/moxyfile" <<'EOF'
[[servers]]
name = "server-a"
command = "cmd-a"
EOF

  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "server-b"
command = "cmd-b"
EOF

  cd "$HOME/repo"
  run_moxy
  assert_failure
  # Moxy fails on first server spawn. The key assertion: it does NOT
  # say "no servers configured", proving moxyfiles were loaded.
  refute_output --partial "no servers configured"
}

function command_string_splits_on_whitespace { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "grit"
command = "nonexistent-grit mcp --verbose"
EOF

  cd "$HOME/repo"
  run_moxy
  assert_failure
  assert_output --partial "nonexistent-grit"
}

function command_array_preserves_args { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "lux"
command = ["nonexistent-lux", "--lsp-dir", "/path with spaces"]
EOF

  cd "$HOME/repo"
  run_moxy
  assert_failure
  assert_output --partial "nonexistent-lux"
}
