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

# Simulates a concurrent push landing on $REMOTE's feat branch behind our back.
simulate_remote_move_on_feat() {
  git clone -q "$REMOTE" "$HOME/other"
  (cd "$HOME/other" &&
    git config user.email t@t &&
    git config user.name t &&
    git config commit.gpgSign false &&
    git checkout -q feat &&
    git commit --allow-empty -m sneak -q &&
    git push -q origin feat)
}

# Simulates a narrow fetch refspec / DWIM checkout: drop the local
# remote-tracking ref for feat, then amend so the boolean force-with-lease
# form has no local baseline to compare the remote against.
drop_feat_tracking_ref_and_amend() {
  git update-ref -d refs/remotes/origin/feat
  git commit --amend --allow-empty -m c1-amended -q
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
  simulate_remote_move_on_feat
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

function push_force_with_lease_boolean_fails_with_stale_info_when_no_tracking_ref { # @test
  cd "$WORK"
  drop_feat_tracking_ref_and_amend
  run "$BIN/push" "origin" "feat" "" true "" "$WORK"
  assert_failure
  assert_output --partial "stale info"
}

function push_force_with_lease_explicit_sha_succeeds_without_tracking_ref { # @test
  cd "$WORK"
  remote_sha=$(git --git-dir="$REMOTE" rev-parse feat)
  # Same missing-tracking-ref situation, but the caller supplies the
  # expected remote SHA explicitly (e.g. fetched via the GitHub API).
  drop_feat_tracking_ref_and_amend
  # arg-order: remote branch set_upstream force_with_lease force_if_includes repo_path remote_branch lease_ref_sha
  run "$BIN/push" "origin" "feat" "" true "" "$WORK" "" "$remote_sha"
  assert_success
}

function push_force_with_lease_explicit_sha_rejects_when_remote_has_moved { # @test
  cd "$WORK"
  remote_sha=$(git --git-dir="$REMOTE" rev-parse feat)
  simulate_remote_move_on_feat
  cd "$WORK"
  drop_feat_tracking_ref_and_amend
  run "$BIN/push" "origin" "feat" "" true "" "$WORK" "" "$remote_sha"
  assert_failure
}

function push_force_with_lease_explicit_sha_succeeds_after_rewritten_history_via_bare_refspec { # @test
  # Reproduces issue #357's exact shape: a branch first pushed via a bare
  # HEAD:remote-branch refspec (no -u, so no tracking ref is ever created),
  # then rebuilt on a fresh base so the new tip does not contain the old
  # remote tip (force_if_includes would also reject this).
  cd "$WORK"
  git checkout -q -b diverged main
  git commit --allow-empty -m d1 -q
  run "$BIN/push" "origin" "diverged" "" "" "" "$WORK" "diverged-remote"
  assert_success
  remote_sha=$(git --git-dir="$REMOTE" rev-parse diverged-remote)
  # Rewrite history: reset onto a fresh base and commit anew, so the new
  # tip's history does not include $remote_sha at all.
  git reset -q --hard main
  git commit --allow-empty -m d1-rewritten -q
  run "$BIN/push" "origin" "diverged" "" true "" "$WORK" "diverged-remote" "$remote_sha"
  assert_success
}
