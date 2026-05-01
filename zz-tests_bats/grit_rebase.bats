#!/usr/bin/env bats

load 'common'

BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin"

setup() {
  setup_test_home
  TMPDIR_TEST="$HOME/repo"
  mkdir -p "$TMPDIR_TEST"
  cd "$TMPDIR_TEST"
  git init -q -b main
  git config user.email t@t
  git config user.name t
  git config commit.gpgSign false
  git commit --allow-empty -m base -q
  git checkout -q -b feat
  git commit --allow-empty -m c1 -q
  git commit --allow-empty -m "fixup! c1" -q
}

teardown() {
  teardown_test_home
}

@test "rebase --autosquash collapses fixup commits" {
  run "$BIN/rebase" main "" "" "" "" "" "" true "" "$TMPDIR_TEST"
  # arg-order: upstream branch autostash continue abort skip onto autosquash update_refs repo_path
  # → upstream=main, autosquash=true (8th positional)
  assert_success
  run git log --oneline main..feat
  assert_success
  # fixup squashed → only one commit above main
  assert_equal "$(echo "$output" | wc -l | tr -d ' ')" "1"
}

@test "rebase --update-refs flag is accepted" {
  run "$BIN/rebase" main "" "" "" "" "" "" "" true "$TMPDIR_TEST"
  assert_success
}

@test "rebase --onto moves the base" {
  git checkout -q main
  git commit --allow-empty -m main2 -q
  git checkout -q feat
  old_main=$(git rev-parse main~1)
  run "$BIN/rebase" "$old_main" "" "" "" "" "" main "" "" "$TMPDIR_TEST"
  # arg-order: upstream=$old_main, onto=main (7th positional)
  assert_success
  assert_equal "$(git merge-base feat main)" "$(git rev-parse main)"
}
