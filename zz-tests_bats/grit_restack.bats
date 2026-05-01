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
  [ "$new_a" != "$old_a" ]
  [ "$new_b" != "$old_b" ]
  [ "$new_c" != "$old_c" ]
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

@test "restack errors with a clear message when root is not a valid ref" {
  cd "$STACK_WORK"
  run "$BIN/restack" main "does-not-exist" "$STACK_WORK"
  assert_failure
  assert_output --partial "cannot resolve root ref"
}

@test "restack failure can be recovered via grit.rebase --abort" {
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
  [ -d "$STACK_WORK/.git/rebase-merge" ] || [ -d "$STACK_WORK/.git/rebase-apply" ]

  # Recover via grit.rebase with do_abort=true (positional 5).
  # arg-order: upstream branch autostash continue abort skip onto autosquash update_refs repo_path
  run "$BIN/rebase" "" "" "" "" true "" "" "" "" "$STACK_WORK"
  assert_success

  # Working tree is clean now and the rebase-merge state is gone.
  [ ! -d "$STACK_WORK/.git/rebase-merge" ]
  [ ! -d "$STACK_WORK/.git/rebase-apply" ]
  cd "$STACK_WORK"
  run git status --porcelain
  assert_success
  assert_output ""
}
