#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function man_page_templates_appear_in_list { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "manpage"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/man://{page}")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/man://{section}/{page}")'
}

function man_page_read_by_name { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "manpage"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"man.resource-read","arguments":{"uri":"man://ls"}}'
  assert_success
  echo "$output" | jq -r '.content[0].text' | jq -r '.[0].text' | grep -qi "ls"
}

function man_page_read_with_section { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "manpage"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"man.resource-read","arguments":{"uri":"man://1/ls"}}'
  assert_success
  echo "$output" | jq -r '.content[0].text' | jq -r '.[0].text' | grep -qi "ls"
}

function man_page_nonexistent_returns_error { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "manpage"
generate-resource-tools = true
EOF

  cd "$HOME/repo"

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local req='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"man.resource-read","arguments":{"uri":"man://nonexistent-page-xyz-12345"}}}'

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy 2>/dev/null | jq -c "select(.id == 2)" | head -1' \
    -- "$init" "$initialized" "$req"
  assert_success
  echo "$output" | jq -e '.error'
}
