#! /usr/bin/env bats

# bats file_tags=gws

# moxy#467: car, prison, gmail, calendar (and gws.api, see gws_api.bats) run
# gws through one shared runner: from a scratch directory, with a failure
# reported as one line. Driven through moxy against the gws stand-in.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  load 'piers_stub'
  setup_test_home
  setup_gws_stub
  mkdir -p "$HOME/project"
}

teardown() {
  teardown_test_home
}

call_tool() { # TOOL ARGUMENTS_JSON
  cd "$HOME/project"
  run_moxy_mcp_v1 "tools/call" "$(jq -cn --arg n "$1" --argjson a "$2" '{name: $n, arguments: $a}')"
  assert_success
}

assert_ok() { jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"; }

function each_tool_makes_its_gws_call { # @test
  local tool args expect
  while IFS='|' read -r tool args expect; do
    : >"$GWS_STUB_LOG"
    call_tool "$tool" "$args"
    assert_ok
    [[ $(cut -f1-3 "$GWS_STUB_LOG" | head -n1) == "$(printf '%b' "$expect")" ]] || fail "$tool called: $(head -n1 "$GWS_STUB_LOG")"
  done <<'EOF'
car.get|{"file_id": "f1"}|drive\tfiles\tget
car.list|{"folder_id": "d1"}|drive\tfiles\tlist
car.search|{"query": "name contains 'x'"}|drive\tfiles\tlist
prison.get|{"spreadsheet_id": "s1"}|sheets\tspreadsheets\tvalues
gmail.read|{"id": "m1"}|gmail\tusers\tmessages
gmail.triage|{}|gmail\tusers\tmessages
calendar.agenda|{}|calendar\tevents\tlist
EOF
}

function gws_runs_from_a_scratch_directory_for_every_tool { # @test
  export GWS_STUB_WRITE_DOWNLOAD=1
  call_tool gmail.triage '{}'
  assert_ok
  call_tool car.get '{"file_id": "f1"}'
  assert_ok
  call_tool calendar.agenda '{}'
  assert_ok
  [[ -z $(ls -A "$HOME/project") ]] || fail "the caller's cwd was written to: $(ls -A "$HOME/project")"
}

function a_gws_failure_is_one_line_naming_the_command { # @test
  export GWS_STUB_FAIL_ALL=1
  local entry
  for entry in 'car.get|{"file_id": "f1"}|gws drive files get' 'gmail.read|{"id": "m1"}|gws gmail users messages' 'prison.get|{"spreadsheet_id": "s1"}|gws sheets spreadsheets values'; do
    IFS='|' read -r tool args want <<<"$entry"
    call_tool "$tool" "$args"
    jq -e '.isError == true' <<<"$output" >/dev/null || fail "$tool: expected an error: $output"
    local text
    text=$(jq -r '.content[0].text' <<<"$output")
    [[ $text == "$want failed: "*"stub boom"* ]] || fail "$tool: $text"
    [[ $text != *$'\n'*$'\n'* ]] || fail "$tool: not one line: $text"
    [[ $text != *ProcessOutput* ]] || fail "$tool: leaked a zx stack: $text"
  done
}

function car_export_reads_the_exported_file_from_the_scratch_directory { # @test
  call_tool car.export '{"file_id": "f1"}'
  assert_ok
  [[ $(jq -r '.content[0].text' <<<"$output") == "exported by the stub" ]] || fail "$output"
  [[ -z $(ls -A "$HOME/project") ]] || fail "the caller's cwd was written to"
}

function car_doc_graph_walks_with_a_relative_output_path { # @test
  call_tool car.doc-graph '{"file_id": "stub-doc", "max_depth": "1"}'
  assert_ok
  [[ $(jq -r '.content[0].text' <<<"$output") == *"Stub Doc"* ]] || fail "$output"
  grep -q $'^drive\tfiles\texport\t' "$GWS_STUB_LOG" || fail "the document was not exported"
  grep -q $'\t--output\texport.html\t' "$GWS_STUB_LOG" || fail "export was not given a path relative to its cwd: $(cat "$GWS_STUB_LOG")"
}
