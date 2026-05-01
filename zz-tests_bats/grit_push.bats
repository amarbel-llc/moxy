#!/usr/bin/env bats

load 'common'

BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin"

setup() {
  setup_test_home
  REMOTE="$HOME/remote.git"
  WORK="$HOME/work"
  git init -q --bare "$REMOTE"
  git init -q -b main "$WORK"
  cd "$WORK"
  git config user.email t@t
  git config user.name t
  git config commit.gpgSign false
  git remote add origin "$REMOTE"
  git commit --allow-empty -m base -q
  git push -q -u origin main
  git checkout -q -b feat
  git commit --allow-empty -m c1 -q
  git push -q -u origin feat
}

teardown() {
  teardown_test_home
}

@test "push --force-with-lease succeeds when remote matches local" {
  cd "$WORK"
  git commit --amend --allow-empty -m c1-amended -q
  run "$BIN/push" "origin" "feat" "" true "$WORK"
  # arg-order: remote branch set_upstream force_with_lease repo_path
  assert_success
}

@test "push --force-with-lease blocks main/master" {
  cd "$WORK"
  run "$BIN/push" "origin" "main" "" true "$WORK"
  assert_failure
  assert_output --partial "force push to main/master is blocked"
}

@test "push --force-with-lease rejects when remote has moved" {
  cd "$WORK"
  git clone -q "$REMOTE" "$HOME/other"
  (cd "$HOME/other" \
    && git config user.email t@t \
    && git config user.name t \
    && git config commit.gpgSign false \
    && git checkout -q feat \
    && git commit --allow-empty -m sneak -q \
    && git push -q origin feat)
  cd "$WORK"
  git commit --amend --allow-empty -m c1-amended -q
  run "$BIN/push" "origin" "feat" "" true "$WORK"
  assert_failure
}
