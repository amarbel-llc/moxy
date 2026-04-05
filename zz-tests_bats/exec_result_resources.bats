#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output

  # Set cache dir inside test home for isolation.
  export XDG_CACHE_HOME="$HOME/.cache"
}

teardown() {
  teardown_test_home
}

function exec_small_output_stays_inline { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/maneater.toml" <<'EOF'
EOF

  cd "$HOME/repo"
  run_maneater_mcp tools/call '{"name":"exec","arguments":{"command":"echo hello"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "hello\n"'
}

function exec_large_output_returns_summary_with_resource_uri { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/maneater.toml" <<'EOF'
EOF

  cd "$HOME/repo"
  # Generate output exceeding 50 token threshold (>200 chars).
  run_maneater_mcp tools/call '{"name":"exec","arguments":{"command":"seq 1 100"}}'
  assert_success

  # Summary should contain the resource URI.
  echo "$output" | jq -er '.content[0].text' | grep -q 'maneater.exec://results/'

  # Summary should contain head/tail markers.
  echo "$output" | jq -er '.content[0].text' | grep -q 'First 10 lines'
  echo "$output" | jq -er '.content[0].text' | grep -q 'Last 10 lines'
}

function exec_resource_read_returns_full_output { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/maneater.toml" <<'EOF'
EOF

  cd "$HOME/repo"
  # Generate large output and extract the resource URI.
  run_maneater_mcp tools/call '{"name":"exec","arguments":{"command":"seq 1 100"}}'
  assert_success

  local uri
  uri=$(echo "$output" | jq -er '.content[0].text' | grep -oP 'maneater\.exec://results/[a-f0-9-]+')

  # Read the resource via a second maneater session.
  local read_params
  read_params=$(jq -cn --arg uri "$uri" '{"uri":$uri}')
  run_maneater_mcp resources/read "$read_params"
  assert_success

  # Full output should contain all 100 lines.
  echo "$output" | jq -er '.contents[0].text' | grep -q '^100$'
  echo "$output" | jq -er '.contents[0].text' | grep -q '^1$'
}
