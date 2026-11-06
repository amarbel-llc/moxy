#! /usr/bin/env bats

# bats file_tags=toolbox

# FDR 0016 (moxy#500, #499 folded in): toolbox.run parses a command of pure
# applets joined by `|`, never a shell. File operands are read resources: the
# gate judges each one, the runner re-asks and opens exactly the judged path
# without following symlinks.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  load "$BATS_TEST_DIRNAME/policy_broker.bash"
  setup_test_home
  PROJECT="$HOME/project"
  mkdir -p "$PROJECT" "$HOME/elsewhere"
  printf 'hello world\n' >"$PROJECT/hello.txt"
  printf '3\n10\n2\n' >"$PROJECT/nums.txt"
  printf 'a b\n' >"$PROJECT/a b.txt"
  echo outside-secret >"$HOME/elsewhere/b.txt"
  ln -s "$HOME/elsewhere/b.txt" "$PROJECT/link"
  TOOLBOX_MOXIN="$MOXIN_PATH/toolbox"
}

teardown() {
  stop_broker
  teardown_test_home
}

# toolbox COMMAND [STDIN]: runs toolbox.run from $PROJECT, sets $output to the
# MCP tool result.
toolbox() {
  local params
  if [[ $# -ge 2 ]]; then
    params=$(jq -cn --arg c "$1" --arg s "$2" '{name: "toolbox.run", arguments: {command: $c, stdin: $s}}')
  else
    params=$(jq -cn --arg c "$1" '{name: "toolbox.run", arguments: {command: $c}}')
  fi
  cd "$PROJECT"
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
}

result_text() { jq -r '.content[0] | (.text // .resource.text // "")' <<<"$output"; }

assert_ok() { jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"; }

assert_error() { jq -e '.isError == true' <<<"$output" >/dev/null || fail "expected an error: $output"; }

# seed_blob TEXT: writes TEXT into the per-test .default store, prints its id.
seed_blob() {
  _ensure_madder_default_store
  printf '%s' "$1" >"$BATS_TEST_TMPDIR/blob"
  (cd "$HOME" && "${MADDER_BIN:-madder}" write -format json .default "$BATS_TEST_TMPDIR/blob") | jq -r 'select(.id) | .id'
}

behavior() { # INPUT_JSON [ADDR]
  hook_behavior mcp__plugin_moxy_moxy__toolbox_run "$1" "${2:-}"
}

function byte_size_and_sha256_of_a_file { # @test
  toolbox "wc -c hello.txt"
  assert_ok
  [[ $(result_text) == "12 /dev/fd/3" ]] || fail "$output"
  local want
  want=$(sha256sum <"$PROJECT/hello.txt" | cut -d' ' -f1)
  toolbox "sha256sum hello.txt"
  assert_ok
  [[ $(result_text) == "$want  /dev/fd/3" ]] || fail "$output"
}

function a_pipeline_runs_without_a_shell { # @test
  toolbox "sort -n nums.txt | tr -d '\n' | wc -c"
  assert_ok
  [[ $(result_text) == 4 ]] || fail "$output"
  # uniq -c pads counts to 7 columns and a space; ties sort -rn as whole lines
  toolbox "tr -s ' ' '\n' | sort | uniq -c | sort -rn | cut -c9-" $'b a b\nc b\n'
  assert_ok
  [[ $(result_text) == $'b\nc\na' ]] || fail "$output"
  toolbox "paste -s -d + -" $'1\n2\n3\n'
  assert_ok
  [[ $(result_text) == 1+2+3 ]] || fail "$output"
  toolbox "wc -l 'a b.txt'"
  assert_ok
  [[ $(result_text) == "1 /dev/fd/3" ]] || fail "$output"
}

function a_blobs_blake2b256_digest_is_its_own_id { # @test
  local id
  id=$(seed_blob "content addressed")
  [[ $id == blake2b256-* ]] || fail "unexpected blob id: $id"
  toolbox "digest -a blake2b256 madder://blobs/$id"
  assert_ok
  [[ $(result_text) == "$id  "* ]] || fail "operand: $output"
  toolbox "digest -a blake2b256" "madder://blobs/$id"
  assert_ok
  [[ $(result_text) == "$id  -" ]] || fail "stdin: $output"
  toolbox "b2sum -l 256 madder://blobs/$id | wc -l"
  assert_ok
  [[ $(result_text) == 1 ]] || fail "b2sum: $output"
}

function shell_syntax_and_unlisted_flags_are_refused_and_nothing_is_written { # @test
  local cmd
  for cmd in "wc -c hello.txt > out" "sort -o out nums.txt" "sort --compress-program=sh nums.txt" \
    "uniq nums.txt out" "wc -c hello.txt; rm hello.txt" 'wc -c $HOME/elsewhere/b.txt' \
    "wc -c *.txt" "cat hello.txt" "sha256sum -c hello.txt"; do
    toolbox "$cmd"
    assert_error
  done
  [[ ! -e $PROJECT/out ]] || fail "a refused command wrote a file"
  [[ -e $PROJECT/hello.txt ]] || fail "a refused command removed a file"
}

function a_symlink_leaf_is_not_followed { # @test
  # the gate judges the link by its own name (inside the worktree); the
  # runner refuses to follow it, so the outside file is never read
  toolbox "wc -c link"
  assert_error
  [[ $output != *outside-secret* ]] || fail "leaked: $output"
  toolbox "sha256sum link"
  assert_error
}

function file_operands_follow_folio_read_under_dont_ask { # @test
  behavior "$(jq -cn --arg p "$HOME/elsewhere/b.txt" '{command: ("wc -c " + $p)}')"
  [[ $HOOK_BEHAVIOR == deny ]] || fail "outside got $HOOK_BEHAVIOR"
  behavior "$(jq -cn --arg p "$PROJECT/hello.txt" '{command: ("wc -c " + $p + " | sort")}')"
  [[ $HOOK_BEHAVIOR == allow ]] || fail "inside got $HOOK_BEHAVIOR"
  behavior "$(jq -cn --arg p "$HOME/elsewhere/b.txt" --arg q "$PROJECT/hello.txt" '{command: ("sort " + $q + " | paste - " + $p)}')"
  [[ $HOOK_BEHAVIOR == deny ]] || fail "outside in a later stage got $HOOK_BEHAVIOR"
  behavior '{"command": "digest madder://blobs/blake2b256-abc"}'
  [[ $HOOK_BEHAVIOR == allow ]] || fail "blob got $HOOK_BEHAVIOR"
  behavior '{"command": "tr a-z A-Z | sort", "stdin": "text"}'
  [[ $HOOK_BEHAVIOR == allow ]] || fail "stdin-only got $HOOK_BEHAVIOR"
  # a tr set is a value, not a path
  behavior "$(jq -cn --arg p "$HOME/elsewhere/b.txt" '{command: ("tr -d " + $p), stdin: "x"}')"
  [[ $HOOK_BEHAVIOR == allow ]] || fail "tr set got $HOOK_BEHAVIOR"
}

function a_missing_path_outside_the_policy_is_refused_and_named { # @test
  behavior "$(jq -cn --arg p "$HOME/elsewhere/missing" '{command: ("wc -c " + $p)}')"
  [[ $HOOK_BEHAVIOR == deny ]] || fail "missing outside path got $HOOK_BEHAVIOR"
  [[ $output == *"$HOME/elsewhere/missing"* ]] || fail "refusal does not name the path: $output"
}

function an_unparseable_command_is_never_allowed { # @test
  local cmd input
  for cmd in "wc -c hello.txt > out" "cat hello.txt" "wc -c 'unterminated" ""; do
    input=$(jq -cn --arg c "$cmd" '{command: $c}')
    behavior "$input"
    [[ $HOOK_BEHAVIOR == deny ]] || fail "no broker, '$cmd' got $HOOK_BEHAVIOR"
  done
  start_broker allow
  for cmd in "wc -c hello.txt > out" "cat hello.txt"; do
    input=$(jq -cn --arg c "$cmd" '{command: $c}')
    behavior "$input" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == deny ]] || fail "allow broker, '$cmd' got $HOOK_BEHAVIOR"
  done
}

function the_broker_judges_each_file_operand { # @test
  start_broker policy sess "$BATS_TEST_TMPDIR/broker.sock" "$PROJECT"
  behavior "$(jq -cn --arg p "$PROJECT/hello.txt" '{command: ("wc -c " + $p)}')" "$BROKER_ADDR"
  [[ $HOOK_BEHAVIOR == allow ]] || fail "inside got $HOOK_BEHAVIOR"
  behavior "$(jq -cn --arg p "$HOME/elsewhere/b.txt" '{command: ("wc -c " + $p)}')" "$BROKER_ADDR"
  [[ $HOOK_BEHAVIOR == deny ]] || fail "outside got $HOOK_BEHAVIOR"
  run broker_checks
  jq -e '.resource.kind == "file"' <<<"${lines[-1]}" >/dev/null || fail "${lines[-1]}"
}

function every_applet_has_a_manpage { # @test
  local applets a
  applets=$("$TOOLBOX_MOXIN/bin/toolbox" applets)
  [[ -n $applets ]] || fail "no applets listed"
  [[ -s $TOOLBOX_MOXIN/share/man/man1/toolbox.1 ]] || fail "toolbox(1) missing"
  for a in $applets; do
    [[ -s $TOOLBOX_MOXIN/share/man/man1/toolbox-$a.1 ]] || fail "toolbox-$a(1) missing"
    [[ -s $TOOLBOX_MOXIN/share/moxy/man/toolbox-$a.1.txt ]] || fail "rendered toolbox-$a(1) missing"
  done
}

# moxy#501: a served moxin's pages are man://<name>.<section> resources, so a
# session with only toolbox can read its manpages without the man moxin.
man_uris() {
  cd "$PROJECT"
  run_moxy_mcp_v1 "resources/list" '{}'
  assert_success
  jq -r '.resources[].uri | select(startswith("man://"))' <<<"$output"
}

function a_served_moxins_pages_are_man_resources { # @test
  run man_uris
  assert_success
  assert_line "man://toolbox.1"
  assert_line "man://toolbox-wc.1"

  cd "$PROJECT"
  run_moxy_mcp_v1 "resources/read" '{"uri": "man://toolbox-wc.1"}'
  assert_success
  jq -e '.contents[0] | .uri == "man://toolbox-wc.1" and .mimeType == "text/plain"' <<<"$output" >/dev/null ||
    fail "$output"
  jq -r '.contents[0].text' <<<"$output" | grep -q 'toolbox-wc - count lines' || fail "not the toolbox-wc page: $output"
  # rendered text, not roff: no overstrike backspaces, no macros
  jq -r '.contents[0].text' <<<"$output" | grep -qE $'\b|^\\.TH' && fail "page is not plain text: $output"
  true
}

function pages_of_a_moxin_not_served_are_not_resources { # @test
  printf 'disable-moxins = ["toolbox"]\n' >"$PROJECT/moxyfile"
  run man_uris
  assert_success
  refute_output --partial "man://toolbox"

  printf 'allow-tools = ["restart"]\n' >"$PROJECT/moxyfile"
  run man_uris
  assert_success
  refute_output --partial "man://toolbox"
  cd "$PROJECT"
  run_moxy_mcp_v1 "resources/read" '{"uri": "man://toolbox-wc.1"}'
  [[ $output != *"count lines, words"* ]] || fail "read a page of a moxin allow-tools hides: $output"
}
