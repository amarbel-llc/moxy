#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function arboretum_outline_returns_a_go_outline_for_a_single_file { # @test
  cat > "$HOME/sample.go" <<'EOF'
package main

func Hello() string { return "hi" }

type Greeter struct {
  Name string
}
EOF

  local params
  params=$(jq -cn --arg n "arboretum.outline" --arg p "$HOME/sample.go" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text // .content[0].text')

  echo "$text" | grep -q "func Hello"
  echo "$text" | grep -q "type Greeter"
  echo "$text" | grep -qE "\[3-3\]"
}

function arboretum_outline_walks_a_directory { # @test
  mkdir -p "$HOME/pkg"
  cat > "$HOME/pkg/a.go" <<'EOF'
package pkg

func A() {}
EOF
  cat > "$HOME/pkg/b.py" <<'EOF'
def b():
    pass
EOF

  local params
  params=$(jq -cn --arg n "arboretum.outline" --arg p "$HOME/pkg" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text // .content[0].text')

  echo "$text" | grep -q "func A"
  echo "$text" | grep -q "def b"
}
