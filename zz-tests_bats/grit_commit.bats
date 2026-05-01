#!/usr/bin/env bats

load 'common'

BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin"

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

@test "commit --allow-empty creates a commit with no changes" {
  cd "$REPO"
  # arg-order: message amend fixup squash allow_empty repo_path
  run "$BIN/commit" "marker" "" "" "" true "$REPO"
  assert_success
  run git log --oneline -2
  assert_success
  # main was the base commit; new commit on top.
  assert_equal "$(echo "$output" | wc -l | tr -d ' ')" "2"
}

@test "commit --fixup creates a fixup! commit targeting the named ref" {
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

@test "commit --squash creates a squash! commit targeting the named ref" {
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

@test "commit --fixup ignores message argument" {
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

@test "commit fails when both fixup and squash are set" {
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

@test "commit fails when message is missing and fixup/squash are not set" {
  cd "$REPO"
  echo "v1" > f.txt
  git add f.txt
  run "$BIN/commit" "" "" "" "" "" "$REPO"
  assert_failure
  assert_output --partial "message is required"
}

@test "commit fixup followed by restack autosquashes the fixup" {
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
