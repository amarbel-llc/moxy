#!/usr/bin/env bats

# bats file_tags=grit

# [explore] Probe whether GIT_SEQUENCE_EDITOR=true silently drops --update-refs.
# Run with: just test-bats-tag grit
# Delete this file when #227 is resolved.

load 'common'

# Creates a minimal stack:
#   main → pr-a (1 commit) → pr-b (1 commit) → pr-c (1 commit)
# Adds a fixup! commit on pr-c targeting pr-a's commit.
# Restacks and asserts branch refs MOVED (proving --update-refs worked).

setup() {
  setup_test_home
  setup_stack_fixture "$HOME"
  cd "$STACK_WORK"
}

teardown() {
  teardown_test_home
}

@test "[explore] GIT_SEQUENCE_EDITOR=true: --update-refs DOES move branch refs" {
  a_sha=$(git rev-parse "$STACK_BRANCH_A")
  git checkout -q "$STACK_BRANCH_C"
  git commit --allow-empty --fixup="$a_sha" -q

  old_a=$(git rev-parse "$STACK_BRANCH_A")
  old_b=$(git rev-parse "$STACK_BRANCH_B")
  old_c=$(git rev-parse "$STACK_BRANCH_C")

  export GIT_EDITOR=true
  export GIT_SEQUENCE_EDITOR=true
  root_sha=$(git rev-parse --verify "$STACK_BRANCH_A")

  git rebase -i --autosquash --update-refs --onto main "${root_sha}^"

  new_a=$(git rev-parse "$STACK_BRANCH_A")
  new_b=$(git rev-parse "$STACK_BRANCH_B")
  new_c=$(git rev-parse "$STACK_BRANCH_C")

  # Assert refs moved (if --update-refs worked, all three should change)
  [ "$new_a" != "$old_a" ] || { echo "FAIL: pr-a SHA unchanged: $old_a (--update-refs not working)" >&2; false; }
  [ "$new_b" != "$old_b" ] || { echo "FAIL: pr-b SHA unchanged: $old_b (--update-refs not working)" >&2; false; }
  [ "$new_c" != "$old_c" ] || { echo "FAIL: pr-c SHA unchanged: $old_c (--update-refs not working)" >&2; false; }
}

@test "[explore] GIT_SEQUENCE_EDITOR=: (colon): --update-refs DOES move branch refs" {
  a_sha=$(git rev-parse "$STACK_BRANCH_A")
  git checkout -q "$STACK_BRANCH_C"
  git commit --allow-empty --fixup="$a_sha" -q

  old_a=$(git rev-parse "$STACK_BRANCH_A")
  old_b=$(git rev-parse "$STACK_BRANCH_B")
  old_c=$(git rev-parse "$STACK_BRANCH_C")

  export GIT_EDITOR=:
  export GIT_SEQUENCE_EDITOR=:
  root_sha=$(git rev-parse --verify "$STACK_BRANCH_A")

  git rebase -i --autosquash --update-refs --onto main "${root_sha}^"

  new_a=$(git rev-parse "$STACK_BRANCH_A")
  new_b=$(git rev-parse "$STACK_BRANCH_B")
  new_c=$(git rev-parse "$STACK_BRANCH_C")

  [ "$new_a" != "$old_a" ] || { echo "FAIL: pr-a SHA unchanged: $old_a (--update-refs not working)" >&2; false; }
  [ "$new_b" != "$old_b" ] || { echo "FAIL: pr-b SHA unchanged: $old_b (--update-refs not working)" >&2; false; }
  [ "$new_c" != "$old_c" ] || { echo "FAIL: pr-c SHA unchanged: $old_c (--update-refs not working)" >&2; false; }
}

@test "[explore] rebase.updateRefs=true config + GIT_SEQUENCE_EDITOR=true: --update-refs moves refs" {
  a_sha=$(git rev-parse "$STACK_BRANCH_A")
  git checkout -q "$STACK_BRANCH_C"
  git commit --allow-empty --fixup="$a_sha" -q

  old_a=$(git rev-parse "$STACK_BRANCH_A")
  old_b=$(git rev-parse "$STACK_BRANCH_B")
  old_c=$(git rev-parse "$STACK_BRANCH_C")

  git config rebase.updateRefs true

  export GIT_EDITOR=true
  export GIT_SEQUENCE_EDITOR=true
  root_sha=$(git rev-parse --verify "$STACK_BRANCH_A")

  git rebase -i --autosquash --update-refs --onto main "${root_sha}^"

  new_a=$(git rev-parse "$STACK_BRANCH_A")
  new_b=$(git rev-parse "$STACK_BRANCH_B")
  new_c=$(git rev-parse "$STACK_BRANCH_C")

  [ "$new_a" != "$old_a" ] || { echo "FAIL: pr-a SHA unchanged: $old_a (--update-refs not working)" >&2; false; }
  [ "$new_b" != "$old_b" ] || { echo "FAIL: pr-b SHA unchanged: $old_b (--update-refs not working)" >&2; false; }
  [ "$new_c" != "$old_c" ] || { echo "FAIL: pr-c SHA unchanged: $old_c (--update-refs not working)" >&2; false; }
}
