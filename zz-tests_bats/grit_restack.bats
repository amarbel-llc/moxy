#!/usr/bin/env bats

# bats file_tags=grit

load 'common'

BIN="${GRIT_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin}"

setup() {
  setup_test_home
  setup_stack_fixture "$HOME"
  cd "$STACK_WORK"
}

teardown() {
  teardown_test_home
}

function restack_autosquashes_a_fixup_against_an_ancestor_branch { # @test
  # Add a fixup on branch C targeting branch A's commit. The fixup MUST carry a
  # real tree change (not --allow-empty): squashing an empty fixup produces no
  # tree change, so git's rebase reuses the original commit objects via its
  # fast-forward optimization and the branch SHAs stay byte-identical even
  # though --update-refs ran correctly. A non-empty payload forces a genuine
  # rewrite of a1 (and its descendants), so the SHA-moved assertions below
  # actually exercise --update-refs.
  a_sha=$(git rev-parse "$STACK_BRANCH_A")
  git checkout -q "$STACK_BRANCH_C"
  echo "fixup payload" > restack_fixup.txt
  git add restack_fixup.txt
  git commit --fixup="$a_sha" -q

  # capture pre-restack SHAs so we can verify --update-refs actually moved them
  old_a=$(git rev-parse "$STACK_BRANCH_A")
  old_b=$(git rev-parse "$STACK_BRANCH_B")
  old_c=$(git rev-parse "$STACK_BRANCH_C")

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

  # --update-refs must have repositioned each branch ref. If --update-refs is
  # silently dropped, the chain assertions above can still pass (B and C are
  # rebased and re-parented), but the local branch refs won't have moved.
  new_a=$(git rev-parse "$STACK_BRANCH_A")
  new_b=$(git rev-parse "$STACK_BRANCH_B")
  new_c=$(git rev-parse "$STACK_BRANCH_C")
  [ "$new_a" != "$old_a" ] \
    || { echo "expected $STACK_BRANCH_A SHA to change after restack; got $old_a → $new_a (--update-refs may be silently dropped)" >&2; false; }
  [ "$new_b" != "$old_b" ] \
    || { echo "expected $STACK_BRANCH_B SHA to change after restack; got $old_b → $new_b (--update-refs may be silently dropped)" >&2; false; }
  [ "$new_c" != "$old_c" ] \
    || { echo "expected $STACK_BRANCH_C SHA to change after restack; got $old_c → $new_c (--update-refs may be silently dropped)" >&2; false; }
}

function restack_refuses_to_run_on_main { # @test
  cd "$STACK_WORK"
  git checkout -q main
  run "$BIN/restack" main main "$STACK_WORK"
  assert_failure
  assert_output --partial "restacking main/master is blocked"
}

function restack_errors_when_onto_is_missing { # @test
  cd "$STACK_WORK"
  run "$BIN/restack" "" "$STACK_BRANCH_A" "$STACK_WORK"
  assert_failure
  assert_output --partial "onto is required"
}

function restack_errors_when_root_is_missing { # @test
  cd "$STACK_WORK"
  run "$BIN/restack" main "" "$STACK_WORK"
  assert_failure
  assert_output --partial "root is required"
}

function restack_errors_with_a_clear_message_when_root_is_not_a_valid_ref { # @test
  cd "$STACK_WORK"
  run "$BIN/restack" main "does-not-exist" "$STACK_WORK"
  assert_failure
  assert_output --partial "cannot resolve root ref"
}

function restack_failure_can_be_recovered_via_grit_rebase_abort { # @test
  cd "$STACK_WORK"
  # Conflict setup: write shared.txt on pr-c, then write the same file with
  # different content on main. When restack replays pr-c onto the new main,
  # git's three-way merge produces an "edit/edit" conflict on shared.txt.
  # (Putting the conflict on pr-c rather than pr-a avoids git's rebase
  # backend silently dropping commits whose effect is already-applied via
  # patch-id equivalence — confirmed empirically: when both main and pr-a
  # modified shared.txt, rebase dropped pr-a's commit and succeeded.)
  git checkout -q "$STACK_BRANCH_C"
  echo "pr-c version" > shared.txt
  git add shared.txt
  git commit -m "pr-c: add shared.txt" -q

  git checkout -q main
  echo "main version" > shared.txt
  git add shared.txt
  git commit -m "main: add shared.txt" -q

  git checkout -q "$STACK_BRANCH_C"

  # Restack should fail with a conflict.
  run "$BIN/restack" main "$STACK_BRANCH_A" "$STACK_WORK"
  assert_failure

  # Verify a rebase is actually in progress (the .git/rebase-merge or
  # .git/rebase-apply directory exists).
  [ -d "$STACK_WORK/.git/rebase-merge" ] || [ -d "$STACK_WORK/.git/rebase-apply" ] \
    || { echo "expected a rebase to be in progress under $STACK_WORK/.git/" >&2; false; }

  # Recover via grit.rebase with do_abort=true (positional 5).
  # arg-order: upstream branch autostash continue abort skip onto autosquash update_refs repo_path
  run "$BIN/rebase" "" "" "" "" true "" "" "" "" "$STACK_WORK"
  assert_success

  # Working tree is clean now and the rebase-merge state is gone.
  [ ! -d "$STACK_WORK/.git/rebase-merge" ] \
    || { echo "rebase-merge directory still exists after grit.rebase --abort" >&2; false; }
  [ ! -d "$STACK_WORK/.git/rebase-apply" ] \
    || { echo "rebase-apply directory still exists after grit.rebase --abort" >&2; false; }
  cd "$STACK_WORK"
  run git status --porcelain
  assert_success
  assert_output ""
}
