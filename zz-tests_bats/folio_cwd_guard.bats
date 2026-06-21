#! /usr/bin/env bats

# bats file_tags=folio
#
# folio's CWD guard was removed in favor of dynamic-perms (see
# bin/folio-perms). The native tools no longer reject paths outside CWD —
# in tests where Claude Code's hook layer doesn't fire, paths anywhere on
# the filesystem are accessible. This file now exercises the still-valid
# in-cwd path and the /dev/fd handling, plus invokes the predicate
# directly to verify its decision policy.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

# ----- folio.read still works for in-CWD paths and /dev/fd -----

function folio_read_allows_file_within_cwd { # @test
  mkdir -p "$HOME/project"
  echo "hello world" > "$HOME/project/test.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "test.txt"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # Small inline content lands in .content[0].text; large content blob
  # cache lands in .content[0].resource.text. Read both and grep.
  echo "$output" \
    | jq -r '.content[0].text // .content[0].resource.text // empty' \
    | grep -q "hello world"
}

function folio_read_allows_dev_fd_path { # @test
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "/dev/fd/0"}}')
  run_moxy_mcp_v1 "tools/call" "$params" <<< "stdin line"
  assert_success
}

function folio_read_now_succeeds_outside_cwd { # @test
  # Native layer no longer rejects outside-CWD paths. Real-world prompting
  # happens at the Claude Code hook layer (dynamic-perms predicate).
  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "accessible" > "$HOME/other/file.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.read" --arg p "$HOME/other/file.txt" \
    '{name: $n, arguments: {file_path: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].text // .content[0].resource.text // empty')
  echo "$text" | grep -q "accessible"
  ! echo "$text" | grep -q "outside CWD"
}

# ----- folio-perms predicate exits as expected -----
#
# These tests invoke the bin/folio-perms script directly to verify its
# decision policy. The script lives inside the nix-built moxin tree.

setup_perms() {
  # In the nix bats lane MOXIN_PATH points at moxy-moxins/share/moxy/moxins;
  # in the devshell it points at result/share/moxy/moxins. Either way,
  # folio-perms lives at $MOXIN_PATH/folio/bin/folio-perms.
  [ -n "${MOXIN_PATH:-}" ] || skip "MOXIN_PATH not set"
  PERMS="$MOXIN_PATH/folio/bin/folio-perms"
  [ -x "$PERMS" ] || skip "folio-perms not at $PERMS"
}

function folio_perms_allows_read_in_cwd { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"
  echo "data" > a.txt

  PWD="$HOME/project" run "$PERMS" read a.txt
  [ "$status" -eq 0 ]
}

function folio_perms_allows_read_in_nix_store { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" read /nix/store
  [ "$status" -eq 0 ]
}

function folio_perms_allows_read_in_dot_claude { # @test
  setup_perms
  mkdir -p "$HOME/project"
  mkdir -p "$HOME/.claude/plans"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" read "$HOME/.claude/plans"
  [ "$status" -eq 0 ]
}

function folio_perms_asks_read_outside_allowed_dirs { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  # Use a path outside CWD, /nix/store, and any Claude-native dir.
  # /var/empty is reliably outside all allow-lists.
  HOME=/dev/null CLAUDE_CODE_TMPDIR= PWD="$HOME/project" \
    run "$PERMS" read /var/empty/secret.txt
  [ "$status" -eq 1 ]
  [[ "$output" == *"confirmation required"* ]]
}

# ----- Sibling-repo reads (git-root relative) -----
#
# folio-perms resolves the current repo's MAIN worktree from the CWD and
# read-allows everything under that worktree's PARENT dir (its sibling
# repos), EXCEPT the main worktree itself. These tests build real git
# repos so the `git rev-parse --git-common-dir` resolution fires.

# Make a one-commit git repo at $1 (identity/signing forced off so it works
# in the sandbox's config-less HOME).
make_repo() {
  git -C "$1" init -q
  git -C "$1" -c user.email=t@t -c user.name=t -c commit.gpgsign=false \
    commit -q --allow-empty -m init
}

function folio_perms_allows_read_in_sibling_repo { # @test
  setup_perms
  mkdir -p "$HOME/repos/myrepo" "$HOME/repos/sibling"
  git -C "$HOME/repos/myrepo" init -q
  echo "data" > "$HOME/repos/sibling/file.txt"
  cd "$HOME/repos/myrepo"

  # CWD is the main worktree; a sibling checkout under the shared parent is
  # read-allowed.
  PWD="$HOME/repos/myrepo" run "$PERMS" read "$HOME/repos/sibling/file.txt"
  [ "$status" -eq 0 ]
}

function folio_perms_allows_sibling_read_from_linked_worktree { # @test
  setup_perms
  mkdir -p "$HOME/repos/myrepo" "$HOME/repos/sibling"
  make_repo "$HOME/repos/myrepo"
  git -C "$HOME/repos/myrepo" worktree add -q "$HOME/repos/myrepo/.worktrees/wt"
  echo "data" > "$HOME/repos/sibling/file.txt"

  # CWD is a spinclass-style linked worktree; the main worktree (hence its
  # sibling parent) is still resolved via --git-common-dir. folio-perms reads
  # $PWD, so the process must actually cd here (a bare PWD= env prefix is
  # recomputed away by bash at startup).
  cd "$HOME/repos/myrepo/.worktrees/wt"
  PWD="$HOME/repos/myrepo/.worktrees/wt" \
    run "$PERMS" read "$HOME/repos/sibling/file.txt"
  [ "$status" -eq 0 ]
}

function folio_perms_asks_read_of_main_worktree_from_linked { # @test
  setup_perms
  mkdir -p "$HOME/repos/myrepo"
  make_repo "$HOME/repos/myrepo"
  git -C "$HOME/repos/myrepo" worktree add -q "$HOME/repos/myrepo/.worktrees/wt"
  echo "secret" > "$HOME/repos/myrepo/secret.txt"

  # The main checkout itself is explicitly excluded — reading it from a
  # linked worktree still prompts (work belongs in the worktree).
  cd "$HOME/repos/myrepo/.worktrees/wt"
  PWD="$HOME/repos/myrepo/.worktrees/wt" \
    run "$PERMS" read "$HOME/repos/myrepo/secret.txt"
  [ "$status" -eq 1 ]
  [[ "$output" == *"confirmation required"* ]]
}

function folio_perms_asks_read_outside_repo_parent { # @test
  setup_perms
  mkdir -p "$HOME/repos/myrepo"
  git -C "$HOME/repos/myrepo" init -q
  # A path outside the repo's parent dir is not a sibling — still prompts.
  mkdir -p "$HOME/elsewhere"
  echo "x" > "$HOME/elsewhere/file.txt"
  cd "$HOME/repos/myrepo"

  PWD="$HOME/repos/myrepo" run "$PERMS" read "$HOME/elsewhere/file.txt"
  [ "$status" -eq 1 ]
  [[ "$output" == *"confirmation required"* ]]
}

function folio_perms_allows_write_in_cwd { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" write a.txt
  [ "$status" -eq 0 ]
}

function folio_perms_denies_write_in_nix_store { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" write /nix/store/foo
  [ "$status" -eq 2 ]
  [[ "$output" == *"immutable"* ]]
}

function folio_perms_asks_write_outside_allowed_dirs { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  HOME=/dev/null CLAUDE_CODE_TMPDIR= PWD="$HOME/project" \
    run "$PERMS" write /var/empty/file.txt
  [ "$status" -eq 1 ]
}

function folio_perms_allows_write_in_plans { # @test
  setup_perms
  mkdir -p "$HOME/project"
  mkdir -p "$HOME/.claude/plans"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" write "$HOME/.claude/plans/x.md"
  [ "$status" -eq 0 ]
}

# Regression: each positional to `write` is exactly one path. Whitespace
# inside a positional must NOT be split — that behavior is reserved for
# the `rm` op (which is called with a single space-separated paths
# string). Bug symptom before this guard: folio.write of a file whose
# content started with `//!` (a Rust doc comment) leaked through moxy's
# arg builder as argv[2], then `write` whitespace-split the leaked
# content and treated `//!` as a path.
function folio_perms_write_does_not_whitespace_split_positional { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  # Single positional, spaces inside. The whole string is a path that
  # resolves outside CWD — exit 1 is expected. The message must name
  # the whole positional, not the first whitespace-split token.
  HOME=/dev/null CLAUDE_CODE_TMPDIR= PWD="$HOME/project" \
    run "$PERMS" write "//! some doc"
  [ "$status" -eq 1 ]
  [[ "$output" == *"//! some doc"* ]]
}

# Regression: the `rm` op is the only one that splits a positional on
# whitespace (because `folio.rm`'s `paths` input is a single string
# containing space-separated paths).
function folio_perms_rm_splits_path_list { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"
  touch a.txt b.txt

  PWD="$HOME/project" run "$PERMS" rm "a.txt b.txt"
  [ "$status" -eq 0 ]
}

function folio_perms_rm_rejects_outside_path_in_list { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"
  touch a.txt

  HOME=/dev/null CLAUDE_CODE_TMPDIR= PWD="$HOME/project" \
    run "$PERMS" rm "a.txt /var/empty/x"
  [ "$status" -eq 1 ]
}

function folio_perms_copy_source_read_dest_write { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  # /nix/store source (read-allowed) → cwd dest (write-allowed) = ok
  PWD="$HOME/project" run "$PERMS" copy /nix/store/foo a.txt
  [ "$status" -eq 0 ]

  # cwd source → /nix/store dest = denied (immutable)
  PWD="$HOME/project" run "$PERMS" copy a.txt /nix/store/foo
  [ "$status" -eq 2 ]
}

function folio_perms_dev_fd_paths_pass { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  # /dev/fd/* is the result-URI substitution pipe; always allowed.
  PWD="$HOME/project" run "$PERMS" read /dev/fd/3
  [ "$status" -eq 0 ]
}

function folio_perms_unknown_op_denied { # @test
  setup_perms

  run "$PERMS" yeet /tmp/x
  [ "$status" -eq 2 ]
}

# ----- Regression: result-symlink canonicalization (#253, #256) -----
#
# A `result` symlink at the worktree root (produced by `nix build`) points
# into /nix/store. The old code called `realpath -m` on the target path,
# which followed the symlink and reported the path as being inside
# /nix/store. The fix: resolve via the parent dir + leaf name so that the
# symlink's own location (inside CWD) governs the policy decision.

function folio_perms_allows_write_of_result_symlink_in_cwd { # @test
  # Regression: folio_rm of the `result` symlink itself must be allowed
  # even though the symlink target resolves into /nix/store. (#253)
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"
  # Create a symlink that mimics `nix build` output.
  ln -s /nix/store/fake-hash-somepkg result

  PWD="$HOME/project" run "$PERMS" write result
  [ "$status" -eq 0 ]
}

function folio_perms_allows_rm_of_result_symlink_in_cwd { # @test
  # Regression: folio_rm of the `result` symlink itself must not be
  # refused just because the symlink target is in /nix/store. (#253)
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"
  ln -s /nix/store/fake-hash-somepkg result

  PWD="$HOME/project" run "$PERMS" rm "result"
  [ "$status" -eq 0 ]
}

function folio_perms_allows_write_when_sibling_result_symlink_exists { # @test
  # Regression: writing a new file in a directory whose sibling is a
  # `result -> /nix/store/...` symlink must be allowed. (#256)
  setup_perms
  mkdir -p "$HOME/project/tests"
  cd "$HOME/project"
  ln -s /nix/store/fake-hash-somepkg result

  # New file directly under tests/ — sibling of result at project root.
  PWD="$HOME/project" run "$PERMS" write "$HOME/project/tests/new_file.bats"
  [ "$status" -eq 0 ]
}

function folio_perms_still_denies_write_into_nix_store_proper { # @test
  # Sanity: an actual path INSIDE /nix/store must still be refused. (#253)
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" write /nix/store/foo/bar
  [ "$status" -eq 2 ]
  [[ "$output" == *"immutable"* ]]
}
