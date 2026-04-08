#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

# plant_session creates a project dir under $HOME/.claude/projects and writes a
# session JSONL inside it. Args: project_dir_name session_id jsonl_lines...
plant_session() {
  local project_dir="$1"
  local session_id="$2"
  shift 2
  local dir="$HOME/.claude/projects/$project_dir"
  mkdir -p "$dir"
  : >"$dir/$session_id.jsonl"
  while [[ $# -gt 0 ]]; do
    printf '%s\n' "$1" >>"$dir/$session_id.jsonl"
    shift
  done
}

function resource_templates_listed { # @test
  run_freud_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "freud://sessions")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "freud://sessions/{project}")'
}

function sessions_empty_returns_empty_list { # @test
  mkdir -p "$HOME/.claude/projects"

  run_freud_mcp resources/read '{"uri":"freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q 'no sessions'
}

function sessions_resolves_project_path_from_jsonl { # @test
  plant_session "-real-foo" "abc123" \
    '{"type":"user","cwd":"/real/foo","message":{"role":"user","content":"hi"}}'

  run_freud_mcp resources/read '{"uri":"freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q 'abc123'
  echo "$text" | grep -q '/real/foo'
  # Resolved path must NOT carry the heuristic marker.
  ! echo "$text" | grep -q 'abc123.*heuristic'
}

function sessions_heuristic_fallback_on_empty_jsonl { # @test
  # Project dir with a JSONL that has no usable cwd (only system messages).
  plant_session "-tmp-noresolve" "def456" \
    '{"type":"permission-mode","permissionMode":"acceptEdits"}'

  run_freud_mcp resources/read '{"uri":"freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q 'def456'
  # Heuristic decode of "-tmp-noresolve" should produce "/tmp/noresolve"
  # and the row should be flagged as heuristic.
  echo "$text" | grep -q '/tmp/noresolve'
  echo "$text" | grep -q 'def456.*heuristic'
}

function sessions_sorted_by_mtime_desc { # @test
  plant_session "-a" "old" \
    '{"type":"user","cwd":"/a","message":{"role":"user","content":"x"}}'
  plant_session "-b" "mid" \
    '{"type":"user","cwd":"/b","message":{"role":"user","content":"x"}}'
  plant_session "-c" "new" \
    '{"type":"user","cwd":"/c","message":{"role":"user","content":"x"}}'

  # Distinct mtimes, ascending: old < mid < new.
  touch -d "2026-01-01 12:00:00" "$HOME/.claude/projects/-a/old.jsonl"
  touch -d "2026-02-01 12:00:00" "$HOME/.claude/projects/-b/mid.jsonl"
  touch -d "2026-03-01 12:00:00" "$HOME/.claude/projects/-c/new.jsonl"

  run_freud_mcp resources/read '{"uri":"freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')

  # Strip any header/footer noise; keep only data lines containing one of
  # our session ids, in order.
  local order
  order=$(echo "$text" | grep -E 'old|mid|new' | grep -oE 'old|mid|new' | tr '\n' ' ')
  [[ $order == "new mid old " ]]
}
