#!/usr/bin/env bats

load 'common'

BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin"

setup() {
  setup_test_home
  setup_stack_fixture "$HOME"
  cd "$STACK_WORK"
}

teardown() {
  teardown_test_home
}

@test "push-stack pushes a clean three-branch chain" {
  cd "$STACK_WORK"
  # amend each branch so each push needs --force-with-lease
  for b in "$STACK_BRANCH_A" "$STACK_BRANCH_B" "$STACK_BRANCH_C"; do
    git checkout -q "$b"
    git commit --amend --allow-empty -m "${b}-amended" -q
  done

  branches_json=$(jq -cn --arg a "$STACK_BRANCH_A" --arg b "$STACK_BRANCH_B" --arg c "$STACK_BRANCH_C" \
    '[$a, $b, $c]')
  # git push writes "To <remote>" / "* [new branch]" progress to stderr;
  # drop it so $output is exclusively the JSON written on stdout.
  run bash -c '"$1" "$2" "$3" "$4" 2>/dev/null' -- \
    "$BIN/push-stack" "$branches_json" origin "$STACK_WORK"
  assert_success
  echo "$output" | jq -e '.phase == "push"'
  echo "$output" | jq -e '.results | length == 3'
  echo "$output" | jq -e '[.results[].status] | all(. == "ok")'
}

@test "push-stack dry-run rejects when remote has diverged" {
  cd "$STACK_WORK"
  # advance branch B's remote ref out from under us
  git clone -q "$STACK_REMOTE" "$HOME/other"
  (cd "$HOME/other" \
    && git config user.email t@t \
    && git config user.name t \
    && git config commit.gpgSign false \
    && git checkout -q "$STACK_BRANCH_B" \
    && git commit --allow-empty -m sneak -q \
    && git push -q origin "$STACK_BRANCH_B")

  cd "$STACK_WORK"
  for b in "$STACK_BRANCH_A" "$STACK_BRANCH_B" "$STACK_BRANCH_C"; do
    git checkout -q "$b"
    git commit --amend --allow-empty -m "${b}-amended" -q
  done

  branches_json=$(jq -cn --arg a "$STACK_BRANCH_A" --arg b "$STACK_BRANCH_B" --arg c "$STACK_BRANCH_C" \
    '[$a, $b, $c]')
  # As above: drop git's stderr so $output is just the JSON on stdout.
  run bash -c '"$1" "$2" "$3" "$4" 2>/dev/null' -- \
    "$BIN/push-stack" "$branches_json" origin "$STACK_WORK"
  assert_failure
  echo "$output" | jq -e '.phase == "dry-run"'
  echo "$output" | jq -e --arg b "$STACK_BRANCH_B" '.results[] | select(.branch == $b and .status == "rejected")'

  # confirm no real pushes happened on branch A: remote tip on A still matches its pre-test push state
  remote_a=$(cd "$STACK_REMOTE" && git rev-parse "refs/heads/$STACK_BRANCH_A")
  cd "$STACK_WORK"
  local_a=$(git rev-parse "$STACK_BRANCH_A")
  [ "$remote_a" != "$local_a" ]
}

@test "push-stack rejects main/master in branches list" {
  cd "$STACK_WORK"
  branches_json=$(jq -cn --arg a "$STACK_BRANCH_A" '[$a, "main"]')
  run "$BIN/push-stack" "$branches_json" origin "$STACK_WORK"
  assert_failure
  assert_output --partial "main/master"
}

@test "push-stack errors on invalid JSON branches input" {
  cd "$STACK_WORK"
  run "$BIN/push-stack" "not-json" origin "$STACK_WORK"
  assert_failure
  assert_output --partial "branches"
}
