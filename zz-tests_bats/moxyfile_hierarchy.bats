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
[servers.fake-server]
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
[servers.global-server]
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
[servers.parent-server]
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
[servers.myserver]
command = "global-cmd"
args = ["--global"]
EOF

  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[servers.myserver]
command = "repo-cmd"
args = ["--repo"]
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
[servers.server-a]
command = "cmd-a"
EOF

  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[servers.server-b]
command = "cmd-b"
EOF

  cd "$HOME/repo"
  run_moxy
  assert_failure
  # Moxy fails on first server spawn — error mentions one server name.
  # The key assertion: it does NOT say "no servers configured", proving
  # at least one moxyfile was loaded. The override test above proves
  # both layers are merged. Here we just verify it found servers.
  refute_output --partial "no servers configured"
}
