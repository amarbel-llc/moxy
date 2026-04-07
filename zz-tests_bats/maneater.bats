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

function asciidoctor_generated_man_page_renders_successfully { # @test
  local fixture_dir
  fixture_dir="$(dirname "$BATS_TEST_FILE")/test-fixtures"

  # Set up a fake man directory containing the pivy-tool fixture (asciidoctor-
  # generated roff that triggers a pandoc parse error through maneater's
  # mandoc -T man pipeline). See https://github.com/amarbel-llc/moxy/issues/27
  mkdir -p "$HOME/man/man1"
  cp "$fixture_dir/pivy-tool.1" "$HOME/man/man1/pivy-tool.1"
  export MANPATH="$HOME/man"

  mkdir -p "$HOME/repo"
  cd "$HOME/repo"

  run_maneater_mcp resources/read '{"uri":"man://pivy-tool"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "NAME"
  echo "$text" | grep -q "DESCRIPTION"
  echo "$text" | grep -q "lines)"
}

function asciidoctor_generated_man_page_reads_section { # @test
  local fixture_dir
  fixture_dir="$(dirname "$BATS_TEST_FILE")/test-fixtures"

  mkdir -p "$HOME/man/man1"
  cp "$fixture_dir/pivy-tool.1" "$HOME/man/man1/pivy-tool.1"
  export MANPATH="$HOME/man"

  mkdir -p "$HOME/repo"
  cd "$HOME/repo"

  run_maneater_mcp resources/read '{"uri":"man://pivy-tool/DESCRIPTION"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -qi "piv"
}

function heuristic_manpath_discovers_project_man_pages { # @test
  local fixture_dir
  fixture_dir="$(dirname "$BATS_TEST_FILE")/test-fixtures"

  # Place a man page in man/man1/ inside the project dir — no MANPATH needed.
  mkdir -p "$HOME/repo/man/man1"
  cp "$fixture_dir/pivy-tool.1" "$HOME/repo/man/man1/pivy-tool.1"

  # Point MANPATH to an empty directory so heuristic is the only way to find it.
  mkdir -p "$HOME/empty-man"
  export MANPATH="$HOME/empty-man"

  cd "$HOME/repo"
  run_maneater_mcp resources/read '{"uri":"man://pivy-tool"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "NAME"
}

function heuristic_manpath_no_auto_disables_discovery { # @test
  local fixture_dir
  fixture_dir="$(dirname "$BATS_TEST_FILE")/test-fixtures"

  mkdir -p "$HOME/repo/man/man1"
  cp "$fixture_dir/pivy-tool.1" "$HOME/repo/man/man1/pivy-tool.1"

  # Disable heuristic probing via config.
  cat >"$HOME/repo/maneater.toml" <<'EOF'
[manpath]
no-auto = true
EOF

  # Point MANPATH to an empty directory so manpath(1) returns only that
  # (pivy-tool won't be found there or in any system default).
  mkdir -p "$HOME/empty-man"
  export MANPATH="$HOME/empty-man"

  cd "$HOME/repo"

  # With heuristics disabled, pivy-tool should not be found (it's only in
  # the project's man/ directory, not in system manpath).
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local req='{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"man://pivy-tool"}}'

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | maneater serve mcp 2>/dev/null | jq -c "select(.id == 2)" | head -1' \
    -- "$init" "$initialized" "$req"
  assert_success
  echo "$output" | jq -e '.error'
}

function godoc_templates_appear_in_list { # @test
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
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/godoc://packages/{package}")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/godoc://packages/{package}/{symbol}")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "man/godoc://packages/{package}/{symbol}/src")'
}

function godoc_package_overview { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"

  run_maneater_mcp resources/read '{"uri":"godoc://packages/fmt"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "package fmt"
  echo "$text" | grep -q "Println"
}

function godoc_symbol_documentation { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"

  run_maneater_mcp resources/read '{"uri":"godoc://packages/fmt/Println"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "func Println"
}

function godoc_symbol_source { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"

  run_maneater_mcp resources/read '{"uri":"godoc://packages/fmt/Println/src"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "func Println"
}

function godoc_multi_segment_package { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"

  run_maneater_mcp resources/read '{"uri":"godoc://packages/encoding/json"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "package json"
  echo "$text" | grep -q "Marshal"
}

function godoc_nonexistent_package_returns_error { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local req='{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"godoc://packages/nonexistent-pkg-xyz-12345"}}'

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | maneater serve mcp 2>/dev/null | jq -c "select(.id == 2)" | head -1' \
    -- "$init" "$initialized" "$req"
  assert_success
  echo "$output" | jq -e '.error'
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
