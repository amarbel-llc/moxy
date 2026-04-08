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
  order=$(echo "$text" | grep -E 'old|mid|new' | grep -v '^SESSION' | grep -oE 'old|mid|new' | tr '\n' ' ')
  [[ $order == "new mid old " ]]
}

function sessions_columnar_format_preserves_full_uuid { # @test
  # Real session ids are 36-char UUIDs. The SESSION column must be wide
  # enough to hold one without truncation — otherwise agents can't copy a
  # full id out of a listing for a subsequent freud://transcript/{id} call.
  local full_uuid="83aad109-1171-4259-8845-ae3b12c3eafc"
  plant_session "-uuid-test" "$full_uuid" \
    '{"type":"user","cwd":"/uuid/test","message":{"role":"user","content":"x"}}'

  run_freud_mcp resources/read '{"uri":"freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')

  # Full 36-char UUID must appear verbatim, with no "…" truncation marker.
  echo "$text" | grep -q "$full_uuid"
  ! echo "$text" | grep -q "83aad109-1171-4259-8845-ae3b1…"
}

function sessions_columnar_format_has_expected_columns { # @test
  plant_session "-real-foo" "abc123" \
    '{"type":"user","cwd":"/real/foo","message":{"role":"user","content":"hi"}}'

  run_freud_mcp resources/read '{"uri":"freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')

  # Header line carries all five columns.
  echo "$text" | head -1 | grep -q 'SESSION'
  echo "$text" | head -1 | grep -q 'LAST ACTIVITY'
  echo "$text" | head -1 | grep -q 'MSGS'
  echo "$text" | head -1 | grep -q 'SIZE'
  echo "$text" | head -1 | grep -q 'PROJECT'
}

function sessions_message_count_matches_line_count { # @test
  # Plant a session with exactly 5 lines, then assert MSGS reads 5.
  plant_session "-msgcount" "fivelines" \
    '{"type":"permission-mode","permissionMode":"acceptEdits"}' \
    '{"type":"user","cwd":"/msgcount","message":{"role":"user","content":"a"}}' \
    '{"type":"assistant","message":{"role":"assistant","content":"b"}}' \
    '{"type":"user","cwd":"/msgcount","message":{"role":"user","content":"c"}}' \
    '{"type":"assistant","message":{"role":"assistant","content":"d"}}'

  run_freud_mcp resources/read '{"uri":"freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')

  # The data row for "fivelines" should have a 5 in the MSGS column.
  echo "$text" | grep 'fivelines' | grep -qE '\b5\b'
}

function sessions_unknown_format_returns_error { # @test
  plant_session "-real-foo" "abc123" \
    '{"type":"user","cwd":"/real/foo","message":{"role":"user","content":"hi"}}'

  run_freud_mcp_full resources/read '{"uri":"freud://sessions?format=tsv"}'
  assert_success
  # The full envelope must be a JSON-RPC error and the message must mention
  # the reserved-for-future-use language.
  echo "$output" | jq -e '.error.message | test("reserved")'
}

function sessions_with_offset_limit_paginates { # @test
  # Plant 5 sessions with distinct mtimes; request the middle two.
  for i in 1 2 3 4 5; do
    plant_session "-p$i" "session$i" \
      "{\"type\":\"user\",\"cwd\":\"/p$i\",\"message\":{\"role\":\"user\",\"content\":\"x\"}}"
    touch -d "2026-04-0$i 12:00:00" "$HOME/.claude/projects/-p$i/session$i.jsonl"
  done

  # Sorted by mtime desc: session5, session4, session3, session2, session1.
  # offset=1, limit=2 → session4, session3.
  run_freud_mcp resources/read '{"uri":"freud://sessions?offset=1&limit=2"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')

  # Exactly two data rows; only session3 and session4 appear.
  echo "$text" | grep -q 'session4'
  echo "$text" | grep -q 'session3'
  ! echo "$text" | grep -q 'session5'
  ! echo "$text" | grep -q 'session2'
  ! echo "$text" | grep -q 'session1'
}

function sessions_head_tail_summary_on_overflow { # @test
  # Configure tiny thresholds via $HOME/freud.toml so progressive disclosure
  # kicks in deterministically.
  cat >"$HOME/freud.toml" <<'EOF'
[list]
max-rows = 2
head-rows = 1
tail-rows = 1
EOF

  # Plant 5 sessions with distinct mtimes, no pagination params.
  for i in 1 2 3 4 5; do
    plant_session "-q$i" "qsess$i" \
      "{\"type\":\"user\",\"cwd\":\"/q$i\",\"message\":{\"role\":\"user\",\"content\":\"x\"}}"
    touch -d "2026-04-0$i 12:00:00" "$HOME/.claude/projects/-q$i/qsess$i.jsonl"
  done

  run_freud_mcp resources/read '{"uri":"freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')

  # Newest (qsess5) and oldest (qsess1) should appear; the middle three should not.
  echo "$text" | grep -q 'qsess5'
  echo "$text" | grep -q 'qsess1'
  ! echo "$text" | grep -q 'qsess2'
  ! echo "$text" | grep -q 'qsess3'
  ! echo "$text" | grep -q 'qsess4'

  # Truncation marker explains how to retrieve the rest.
  echo "$text" | grep -q '5 sessions total'
  echo "$text" | grep -qE 'offset=.*limit='
}

function sessions_by_project_dirname_filters_correctly { # @test
  plant_session "-foo" "f1" \
    '{"type":"user","cwd":"/foo","message":{"role":"user","content":"x"}}'
  plant_session "-bar" "b1" \
    '{"type":"user","cwd":"/bar","message":{"role":"user","content":"x"}}'

  run_freud_mcp resources/read '{"uri":"freud://sessions/-foo"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q 'f1'
  ! echo "$text" | grep -q 'b1'
}

function sessions_by_project_abspath_filters_correctly { # @test
  plant_session "-real-foo" "rf1" \
    '{"type":"user","cwd":"/real/foo","message":{"role":"user","content":"x"}}'
  plant_session "-real-bar" "rb1" \
    '{"type":"user","cwd":"/real/bar","message":{"role":"user","content":"x"}}'

  # URL-encoded /real/foo → %2Freal%2Ffoo
  run_freud_mcp resources/read '{"uri":"freud://sessions/%2Freal%2Ffoo"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q 'rf1'
  ! echo "$text" | grep -q 'rb1'
}

function sessions_by_unknown_project_returns_hint { # @test
  plant_session "-known" "k1" \
    '{"type":"user","cwd":"/known","message":{"role":"user","content":"x"}}'

  run_freud_mcp_full resources/read '{"uri":"freud://sessions/-nonexistent"}'
  assert_success
  # Error message should mention the unknown project and list known ones.
  echo "$output" | jq -e '.error.message | test("nonexistent")'
  echo "$output" | jq -e '.error.message | test("-known")'
}

function unknown_uri_returns_hint { # @test
  run_freud_mcp_full resources/read '{"uri":"freud://nope"}'
  assert_success
  # Error message should suggest the valid entry points.
  echo "$output" | jq -e '.error.message | test("freud://sessions")'
}

function transcript_template_listed { # @test
  run_freud_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "freud://transcript/{session_id}")'
}

function transcript_returns_raw_jsonl_content { # @test
  plant_session "-tx-foo" "txid1" \
    '{"type":"user","cwd":"/tx/foo","message":{"role":"user","content":"first"}}' \
    '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"reply one"}]}}' \
    '{"type":"user","cwd":"/tx/foo","message":{"role":"user","content":"second"}}'

  run_freud_mcp resources/read '{"uri":"freud://transcript/txid1"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')

  # Raw passthrough: every planted line should appear verbatim.
  echo "$text" | grep -q '"content":"first"'
  echo "$text" | grep -q '"text":"reply one"'
  echo "$text" | grep -q '"content":"second"'
}

function transcript_unknown_id_returns_hint { # @test
  plant_session "-tx-foo" "knownid" \
    '{"type":"user","cwd":"/tx/foo","message":{"role":"user","content":"x"}}'

  run_freud_mcp_full resources/read '{"uri":"freud://transcript/nonexistent-id"}'
  assert_success
  # Error must name the missing id and point at the discovery entry point.
  echo "$output" | jq -e '.error.message | test("nonexistent-id")'
  echo "$output" | jq -e '.error.message | test("freud://sessions")'
}

function transcript_finds_session_across_projects { # @test
  # Plant the same session id under two different project dirs to confirm
  # the lookup walks all projects, not just the first one alphabetically.
  plant_session "-aaa-first" "sentinel-1" \
    '{"type":"user","cwd":"/aaa/first","message":{"role":"user","content":"alpha"}}'
  plant_session "-zzz-second" "sentinel-2" \
    '{"type":"user","cwd":"/zzz/second","message":{"role":"user","content":"omega"}}'

  run_freud_mcp resources/read '{"uri":"freud://transcript/sentinel-2"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  # Must find the second session, not the first.
  echo "$text" | grep -q 'omega'
  ! echo "$text" | grep -q 'alpha'
}

# Integration test: freud running as a child of moxy. Confirms templates and
# resource reads come through with the moxy "freud/" namespace prefix and
# that the planted session is visible end-to-end.
function freud_served_through_moxy_proxy { # @test
  plant_session "-real-foo" "proxy_id" \
    '{"type":"user","cwd":"/real/foo","message":{"role":"user","content":"hi"}}'

  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "freud"
command = ["freud", "serve", "mcp"]
EOF

  cd "$HOME/repo"

  run_moxy_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "freud/freud://sessions")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "freud/freud://sessions/{project}")'

  run_moxy_mcp resources/read '{"uri":"freud/freud://sessions"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q 'proxy_id'
  echo "$text" | grep -q '/real/foo'
}
