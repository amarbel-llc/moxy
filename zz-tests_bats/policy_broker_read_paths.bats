#! /usr/bin/env bats

# bats file_tags=policy_broker

# moxy#490: every tool that reads a path the caller names asks the same policy
# folio_read does (the broker, then folio-perms as the floor), so a path
# folio_read denies is denied for the others too. The stand-in broker's
# "policy" mode plays the spinclass stanza: allow under the worktree, never
# under .spinclass/ except spawn.log, blobs only when listed.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  load "$BATS_TEST_DIRNAME/policy_broker.bash"
  setup_test_home
  PROJECT="$HOME/project"
  mkdir -p "$PROJECT/.spinclass" "$HOME/elsewhere"
  echo needle >"$PROJECT/a.txt"
  echo needle >"$HOME/elsewhere/b.txt"
  echo needle >"$PROJECT/.spinclass/spawn.log"
  echo secret >"$PROJECT/.spinclass/token"
  echo '{}' >"$PROJECT/j.json"
}

teardown() {
  stop_broker
  teardown_test_home
}

# tool_input TOOL PATH: the input object naming PATH as that tool's read path.
tool_input() {
  case "$1" in
    folio_read) jq -cn --arg p "$2" '{file_path: $p}' ;;
    rg_search) jq -cn --arg p "$2" '{pattern: "needle", path: $p}' ;;
    jq_jq) jq -cn --arg p "$2" '{filter: ".", files: [$p]}' ;;
    arboretum_search) jq -cn --arg p "$2" '{pattern: "x", path: $p}' ;;
    arboretum_outline) jq -cn --arg p "$2" '{path: $p}' ;;
    chix_nix-hash) jq -cn --arg p "$2" '{path: $p}' ;;
    piers_create) jq -cn --arg p "$2" '{title: "t", content_path: $p}' ;;
    piers_update) jq -cn --arg p "$2" '{file_id: "f", content_path: $p}' ;;
    *) jq -cn --arg p "$2" '{path: $p}' ;;
  esac
}

behavior() { # TOOL PATH [ADDR]
  hook_behavior "mcp__plugin_moxy_moxy__$1" "$(tool_input "$1" "$2")" "${3:-}"
}

# the tools that read the path they are given, folio_read being the reference
READ_TOOLS="folio_read rg_search folio_glob folio_ls folio_tree folio_du folio_file-type"
# always-allow tools that read a named path and carried no policy (#490)
READ_TOOLS_ALSO="jq_jq arboretum_search arboretum_outline chix_nix-hash piers_create piers_update"

function the_other_path_reading_tools_follow_folio_read { # @test
  local t
  for t in $READ_TOOLS_ALSO; do
    behavior "$t" "$HOME/elsewhere/b.txt"
    [[ $HOOK_BEHAVIOR == deny ]] || fail "$t: outside the worktree got $HOOK_BEHAVIOR"
    behavior "$t" "$PROJECT/a.txt"
    [[ $HOOK_BEHAVIOR == allow ]] || fail "$t: inside the worktree got $HOOK_BEHAVIOR"
  done
  start_broker policy sess "$BATS_TEST_TMPDIR/broker.sock" "$PROJECT"
  for t in $READ_TOOLS_ALSO; do
    behavior "$t" "$PROJECT/.spinclass/token" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == deny ]] || fail "$t: broker-denied path got $HOOK_BEHAVIOR"
  done
}

function without_a_broker_every_read_tool_has_the_floor_of_folio_read { # @test
  local t
  for t in $READ_TOOLS; do
    behavior "$t" "$HOME/elsewhere/b.txt"
    [[ $HOOK_BEHAVIOR == deny ]] || fail "$t: outside the worktree got $HOOK_BEHAVIOR"
    behavior "$t" "$PROJECT/a.txt"
    [[ $HOOK_BEHAVIOR == allow ]] || fail "$t: inside the worktree got $HOOK_BEHAVIOR"
  done
}

function the_broker_verdict_is_the_same_for_every_read_tool { # @test
  start_broker policy sess "$BATS_TEST_TMPDIR/broker.sock" "$PROJECT"
  local t
  for t in $READ_TOOLS; do
    behavior "$t" "$HOME/elsewhere/b.txt" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == deny ]] || fail "$t: denied path got $HOOK_BEHAVIOR"
    behavior "$t" "$PROJECT/a.txt" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == allow ]] || fail "$t: allowed path got $HOOK_BEHAVIOR"
    behavior "$t" "$PROJECT/.spinclass/spawn.log" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == allow ]] || fail "$t: spawn.log got $HOOK_BEHAVIOR"
  done
}

function a_broker_denial_beats_a_floor_that_would_allow { # @test
  start_broker policy sess "$BATS_TEST_TMPDIR/broker.sock" "$PROJECT"
  local t
  for t in $READ_TOOLS; do
    # inside the worktree (the floor allows), but under .spinclass/ (the policy denies)
    behavior "$t" "$PROJECT/.spinclass/token" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == deny ]] || fail "$t: .spinclass/token got $HOOK_BEHAVIOR"
  done
}

function a_broker_allow_beats_a_floor_that_would_ask { # @test
  start_broker policy sess "$BATS_TEST_TMPDIR/broker.sock" "$HOME/elsewhere"
  local t
  for t in $READ_TOOLS; do
    behavior "$t" "$HOME/elsewhere/b.txt" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == allow ]] || fail "$t: broker-allowed path got $HOOK_BEHAVIOR"
  done
}

function a_blob_uri_is_asked_as_a_blob { # @test
  local blob="madder://blobs/blake2b256-abc"
  start_broker policy sess "$BATS_TEST_TMPDIR/broker.sock" "$PROJECT"
  behavior rg_search "$blob" "$BROKER_ADDR"
  [[ $HOOK_BEHAVIOR == deny ]] || fail "a blob the policy withholds got $HOOK_BEHAVIOR"
  run broker_checks
  jq -e '.resource.kind == "blob" and .resource.uri == "'"$blob"'"' <<<"${lines[-1]}"
  stop_broker
  start_broker policy sess "$BATS_TEST_TMPDIR/broker.sock" "$PROJECT,blob"
  behavior rg_search "$blob" "$BROKER_ADDR"
  [[ $HOOK_BEHAVIOR == allow ]] || fail "a blob the policy lists got $HOOK_BEHAVIOR"
}

function an_unanswered_broker_leaves_every_read_tool_on_its_floor { # @test
  start_broker hang
  local t
  for t in $READ_TOOLS; do
    behavior "$t" "$HOME/elsewhere/b.txt" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == deny ]] || fail "$t: outside got $HOOK_BEHAVIOR"
    behavior "$t" "$PROJECT/a.txt" "$BROKER_ADDR"
    [[ $HOOK_BEHAVIOR == allow ]] || fail "$t: inside got $HOOK_BEHAVIOR"
  done
}
