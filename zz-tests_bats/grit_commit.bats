#!/usr/bin/env bats

# bats file_tags=grit

load 'common'

BIN="${GRIT_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin}"

setup() {
  setup_test_home
  REPO="$HOME/repo"
  mkdir -p "$REPO"
  cd "$REPO"
  git init -q -b main
  git config user.email t@t
  git config user.name t
  git config commit.gpgSign false
  git commit --allow-empty -m base -q
}

teardown() {
  teardown_test_home
}

function commit_allow_empty_creates_a_commit_with_no_changes { # @test
  cd "$REPO"
  # arg-order: message amend fixup squash allow_empty repo_path
  run "$BIN/commit" "marker" "" "" "" true "$REPO"
  assert_success
  run git log --oneline -2
  assert_success
  # main was the base commit; new commit on top.
  assert_equal "$(echo "$output" | wc -l | tr -d ' ')" "2"
}

function commit_fixup_creates_a_fixup_commit_targeting_the_named_ref { # @test
  cd "$REPO"
  echo "v1" > f.txt
  git add f.txt
  git commit -m "add f" -q
  target=$(git rev-parse HEAD)
  echo "v2" > f.txt
  git add f.txt
  run "$BIN/commit" "" "" "$target" "" "" "$REPO"
  assert_success
  run git log --oneline -1
  assert_success
  assert_output --partial "fixup! add f"
}

function commit_squash_creates_a_squash_commit_targeting_the_named_ref { # @test
  cd "$REPO"
  echo "v1" > f.txt
  git add f.txt
  git commit -m "add f" -q
  target=$(git rev-parse HEAD)
  echo "v2" > f.txt
  git add f.txt
  run "$BIN/commit" "" "" "" "$target" "" "$REPO"
  assert_success
  run git log --oneline -1
  assert_success
  assert_output --partial "squash! add f"
}

function commit_fixup_ignores_message_argument { # @test
  cd "$REPO"
  echo "v1" > f.txt
  git add f.txt
  git commit -m "add f" -q
  target=$(git rev-parse HEAD)
  echo "v2" > f.txt
  git add f.txt
  # message is "irrelevant" — should be ignored when fixup is set
  run "$BIN/commit" "irrelevant" "" "$target" "" "" "$REPO"
  assert_success
  run git log --oneline -1
  assert_success
  assert_output --partial "fixup! add f"
  refute_output --partial "irrelevant"
}

function commit_fails_when_both_fixup_and_squash_are_set { # @test
  cd "$REPO"
  echo "v1" > f.txt
  git add f.txt
  git commit -m "add f" -q
  target=$(git rev-parse HEAD)
  echo "v2" > f.txt
  git add f.txt
  run "$BIN/commit" "" "" "$target" "$target" "" "$REPO"
  assert_failure
  assert_output --partial "mutually exclusive"
}

function commit_fails_when_message_is_missing_and_fixup_squash_are_not_set { # @test
  cd "$REPO"
  echo "v1" > f.txt
  git add f.txt
  run "$BIN/commit" "" "" "" "" "" "$REPO"
  assert_failure
  assert_output --partial "message is required"
}

function commit_fixup_followed_by_restack_autosquashes_the_fixup { # @test
  cd "$REPO"
  # build a tiny stack: main → A → B, then add a fixup on B targeting A.
  git checkout -q -b A
  echo "a content" > a.txt
  git add a.txt
  git commit -m "add a.txt" -q
  a_sha=$(git rev-parse HEAD)

  git checkout -q -b B
  echo "b content" > b.txt
  git add b.txt
  git commit -m "add b.txt" -q

  # fixup on B targeting A. Use grit.commit --fixup (the new flag).
  echo "a content with annotation" > a.txt
  git add a.txt
  run "$BIN/commit" "" "" "$a_sha" "" "" "$REPO"
  assert_success

  # restack: replay everything from A onto main, with autosquash absorbing the fixup.
  run "$BIN/restack" main A "$REPO"
  assert_success

  # A should still have exactly one commit above main (the fixup absorbed into a).
  run git log --oneline "main..A"
  assert_success
  assert_equal "$(echo "$output" | wc -l | tr -d ' ')" "1"

  # And A's a.txt now contains the annotation (proving the fixup's content landed).
  cd "$REPO"
  git checkout -q A
  run cat a.txt
  assert_output "a content with annotation"
}

# Regression for the #366-class blob-URI substitution pathology (tracked
# systemically in #371): moxy must not rewrite madder://blobs/<digest> URIs
# that appear inside grit.commit's `message`. The commit message is verbatim
# data, never a blob to stream, so the read-side /dev/fd/N rewrite only
# corrupts it. This MUST route through the moxy proxy (run_moxy_mcp_v1) so
# substituteArgvBlobURIs actually fires — invoking $BIN/commit directly
# bypasses moxy's arg layer and would not catch the bug.
function commit_message_with_madder_uri_is_written_verbatim { # @test
  cd "$REPO"
  echo "change" > tracked.txt
  git add tracked.txt

  local msg='ref madder://blobs/blake2b256-deadbeefcafe and madder://blobs/blake2b256-secondref'
  local params
  params=$(jq -cn --arg n "grit.commit" --arg m "$msg" --arg r "$REPO" \
    '{name: $n, arguments: {message: $m, repo_path: $r}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # The recorded commit message must be the bytes we passed — no /dev/fd/N.
  # $(...) strips the trailing newline %B appends, leaving the verbatim line.
  local body
  body=$(git -C "$REPO" log -1 --format=%B)
  assert_equal "$body" "$msg"
}
