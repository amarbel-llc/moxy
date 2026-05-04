#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function arboretum_search_finds_a_pattern_in_a_js_file { # @test
  cat > "$HOME/app.js" <<'EOF'
console.log("startup");
console.log("done");
function inner() {
  console.log("hi", "world");
}
EOF

  local params
  params=$(jq -cn --arg n "arboretum.search" --arg p "console.log(\$MSG)" --arg path "$HOME" \
    '{name: $n, arguments: {pattern: $p, path: $path}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text // .content[0].text')

  echo "$text" | grep -qE 'console\.log\("startup"\)'
  echo "$text" | grep -qE 'console\.log\("done"\)'
  # The 2-arg form should NOT match the single-metavar pattern.
  if echo "$text" | grep -qE 'console\.log\("hi"'; then
    echo "single-arg pattern incorrectly matched two-arg call" >&2
    return 1
  fi
}

function arboretum_search_matches_go_call_via_auto_wrap { # @test
  # Go's grammar is parser-ambiguous for `fmt.Println($X)` patterns at top
  # level (tree-sitter-go prefers type_conversion over call_expression). The
  # arboretum wrapper detects lang=go + call-shaped pattern and synthesizes
  # a YAML rule with context + `selector: call_expression` via ast-grep scan
  # --inline-rules. See moxins/arboretum/src/astgrep.ts for the heuristic
  # and https://ast-grep.github.io/catalog/go/#match-function-call-in-golang
  # for the underlying issue.
  cat > "$HOME/main.go" <<'EOF'
package main

import "fmt"

func main() {
  fmt.Println("hi")
  fmt.Println("there")
}
EOF

  local params
  params=$(jq -cn --arg n "arboretum.search" --arg p "fmt.Println(\$X)" --arg path "$HOME" --arg lang "go" \
    '{name: $n, arguments: {pattern: $p, path: $path, lang: $lang}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text // .content[0].text')

  echo "$text" | grep -qE 'fmt\.Println\("hi"\)'
  echo "$text" | grep -qE 'fmt\.Println\("there"\)'
}
