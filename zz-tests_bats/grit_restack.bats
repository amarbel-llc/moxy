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

@test "restack autosquashes a fixup against an ancestor branch" {
  # add a fixup on branch C targeting branch A's commit
  a_sha=$(git rev-parse "$STACK_BRANCH_A")
  git checkout -q "$STACK_BRANCH_C"
  git commit --allow-empty --fixup="$a_sha" -q

  # restack onto main, root = pr-a (so the rebase replays everything from pr-a's parent forward)
  run "$BIN/restack" main "$STACK_BRANCH_A" "$STACK_WORK"
  assert_success

  # The fixup commit should be absorbed: branch A still has exactly 1 commit above main.
  run git log --oneline "main..$STACK_BRANCH_A"
  assert_success
  assert_equal "$(echo "$output" | wc -l | tr -d ' ')" "1"

  # And the chain is preserved: B's parent is A, C's parent is B.
  cd "$STACK_WORK"
  assert_equal "$(git rev-parse $STACK_BRANCH_B^)" "$(git rev-parse $STACK_BRANCH_A)"
  assert_equal "$(git rev-parse $STACK_BRANCH_C^)" "$(git rev-parse $STACK_BRANCH_B)"
}

@test "restack refuses to run on main" {
  cd "$STACK_WORK"
  git checkout -q main
  run "$BIN/restack" main main "$STACK_WORK"
  assert_failure
  assert_output --partial "restacking main/master is blocked"
}

@test "restack errors when onto is missing" {
  cd "$STACK_WORK"
  run "$BIN/restack" "" "$STACK_BRANCH_A" "$STACK_WORK"
  assert_failure
  assert_output --partial "onto is required"
}

@test "restack errors when root is missing" {
  cd "$STACK_WORK"
  run "$BIN/restack" main "" "$STACK_WORK"
  assert_failure
  assert_output --partial "root is required"
}
