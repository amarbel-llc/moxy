#!/usr/bin/env bats

# bats file_tags=grit

load 'common'

BIN="${GRIT_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin}"

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

function push_force_with_lease_succeeds_when_remote_matches_local { # @test
  cd "$WORK"
  git commit --amend --allow-empty -m c1-amended -q
  run "$BIN/push" "origin" "feat" "" true "" "$WORK"
  # arg-order: remote branch set_upstream force_with_lease force_if_includes repo_path
  assert_success
}

function push_force_with_lease_blocks_main_master { # @test
  cd "$WORK"
  run "$BIN/push" "origin" "main" "" true "" "$WORK"
  assert_failure
  assert_output --partial "force push to main/master is blocked"
}

function push_force_with_lease_blocks_detached_HEAD_with_no_branch_arg { # @test
  cd "$WORK"
  # detach HEAD
  git checkout -q --detach
  # remote="origin", branch="" (empty), set_upstream="", force_with_lease=true
  run "$BIN/push" "origin" "" "" true "" "$WORK"
  assert_failure
  assert_output --partial "explicit branch argument"
}

function push_force_with_lease_rejects_when_remote_has_moved { # @test
  cd "$WORK"
  git clone -q "$REMOTE" "$HOME/other"
  (cd "$HOME/other" &&
    git config user.email t@t &&
    git config user.name t &&
    git config commit.gpgSign false &&
    git checkout -q feat &&
    git commit --allow-empty -m sneak -q &&
    git push -q origin feat)
  cd "$WORK"
  git commit --amend --allow-empty -m c1-amended -q
  run "$BIN/push" "origin" "feat" "" true "" "$WORK"
  assert_failure
}

function push_force_with_lease_force_if_includes_succeeds_when_commit_includes_remote_tip { # @test
  cd "$WORK"
  # amend the local commit (includes the prior remote tip in its history via parent)
  git commit --amend --allow-empty -m c1-amended -q
  run "$BIN/push" "origin" "feat" "" true true "$WORK"
  # arg-order: remote branch set_upstream force_with_lease force_if_includes repo_path
  assert_success
}

function push_force_if_includes_alone_passes_flag_without_force_with_lease { # @test
  cd "$WORK"
  # A plain push with force_if_includes=true but force_with_lease=false
  # git itself treats --force-if-includes as a no-op without --force, so push succeeds normally
  run "$BIN/push" "origin" "feat" "" "" true "$WORK"
  assert_success
}

function push_refspec_pushes_local_branch_to_differently_named_remote_branch { # @test
  cd "$WORK"
  # arg-order: remote branch set_upstream force_with_lease force_if_includes repo_path remote_branch
  run "$BIN/push" "origin" "feat" "" "" "" "$WORK" "renamed-feat"
  assert_success
  run git --git-dir="$REMOTE" rev-parse --verify renamed-feat
  assert_success
}

function push_refspec_head_to_named_remote_branch { # @test
  cd "$WORK"
  # branch empty + remote_branch set -> pushes HEAD:from-head
  run "$BIN/push" "origin" "" "" "" "" "$WORK" "from-head"
  assert_success
  run git --git-dir="$REMOTE" rev-parse --verify from-head
  assert_success
}

function push_refspec_defaults_remote_to_origin { # @test
  cd "$WORK"
  # remote empty, remote_branch set -> origin still resolved
  run "$BIN/push" "" "feat" "" "" "" "$WORK" "default-remote"
  assert_success
  run git --git-dir="$REMOTE" rev-parse --verify default-remote
  assert_success
}

function push_refspec_force_with_lease_blocks_main_destination { # @test
  cd "$WORK"
  # destination remote_branch=main must be blocked even when source is a feature branch
  run "$BIN/push" "origin" "feat" "" true "" "$WORK" "main"
  assert_failure
  assert_output --partial "force push to main/master is blocked"
}
