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
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/man://{page}")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/man://{section}/{page}")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/man://{page}/{section_name}")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/man://{section}/{page}/{section_name}")'
}

function man_page_toc_by_name { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"man.resource-read","arguments":{"uri":"man://ls"}}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -q "NAME"
  echo "$text" | grep -q "DESCRIPTION"
  echo "$text" | grep -q "lines)"
}

function man_page_toc_with_section { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"man.resource-read","arguments":{"uri":"man://1/ls"}}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -q "NAME"
  echo "$text" | grep -q "DESCRIPTION"
  echo "$text" | grep -q "lines)"
}

function man_page_read_section { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"man.resource-read","arguments":{"uri":"man://ls/SYNOPSIS"}}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -qi "ls"
  # TOC should not appear in section content
  echo "$text" | grep -qv "lines)"
}

function man_page_read_section_with_man_section { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"man.resource-read","arguments":{"uri":"man://1/ls/DESCRIPTION"}}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -qi "directory"
}

function man_page_toc_includes_subsections { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"man.resource-read","arguments":{"uri":"man://jq"}}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  # jq has subsections indented with 2 spaces in TOC
  echo "$text" | grep -q "^  "
  echo "$text" | grep -q "BASIC FILTERS"
}

function man_page_nonexistent_section_returns_error { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
generate-resource-tools = true
EOF

  cd "$HOME/repo"

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local req='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"man.resource-read","arguments":{"uri":"man://ls/NONEXISTENT"}}}'

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy 2>/dev/null | jq -c "select(.id == 2)" | head -1' \
    -- "$init" "$initialized" "$req"
  assert_success
  echo "$output" | jq -e '.error'
}

function search_template_appears_in_list { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
generate-resource-tools = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/man://search/{query}")'
}

function man_page_nonexistent_returns_error { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "man"
command = "maneater serve mcp"
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
