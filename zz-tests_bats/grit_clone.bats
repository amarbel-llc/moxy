#! /usr/bin/env bats

# bats file_tags=grit

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

  export XDG_CACHE_HOME="$HOME/.cache"

  # Source repo to clone from: two commits on main plus a `feature` branch.
  mkdir -p "$HOME/src"
  cd "$HOME/src"
  git init -q -b main
  git config user.email "test@test.com"
  git config user.name "Test"
  git config commit.gpgSign false

  echo "one" >file.txt
  git add file.txt
  git commit -q -m "first"

  echo "two" >file.txt
  git add file.txt
  git commit -q -m "second"

  git checkout -q -b feature
  echo "feat" >feature.txt
  git add feature.txt
  git commit -q -m "feature commit"
  git checkout -q main

  cd "$HOME"
}

teardown() {
  teardown_test_home
}

# Extract the text payload from a tools/call result (inline or blob-cached).
clone_result_text() {
  echo "$output" | jq -r '.content[0].text // .content[0].resource.text // empty'
}

function grit_clone_into_cwd_subdir { # @test
  local params
  params=$(jq -cn --arg s "$HOME/src" --arg d "$HOME/dest" \
    '{name: "grit.clone", arguments: {source: $s, destination: $d}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ -d "$HOME/dest/.git" ]
  [ -f "$HOME/dest/file.txt" ]
  grep -q "two" "$HOME/dest/file.txt"
}

function grit_clone_with_branch { # @test
  local params
  params=$(jq -cn --arg s "$HOME/src" --arg d "$HOME/feat" \
    '{name: "grit.clone", arguments: {source: $s, destination: $d, branch: "feature"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ "$(git -C "$HOME/feat" rev-parse --abbrev-ref HEAD)" = "feature" ]
  [ -f "$HOME/feat/feature.txt" ]
}

function grit_clone_shallow_depth { # @test
  # git honors --depth only over a transport, not a local-path clone (which
  # hardlinks the object store and ignores depth). Use a file:// URL so the
  # depth flag actually takes effect and proves it is wired through.
  local params
  params=$(jq -cn --arg s "file://$HOME/src" --arg d "$HOME/shallow" \
    '{name: "grit.clone", arguments: {source: $s, destination: $d, depth: 1}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ "$(git -C "$HOME/shallow" rev-parse --is-shallow-repository)" = "true" ]
  [ "$(git -C "$HOME/shallow" rev-list --count HEAD)" = "1" ]
}

function grit_clone_bare { # @test
  local params
  params=$(jq -cn --arg s "$HOME/src" --arg d "$HOME/bare.git" \
    '{name: "grit.clone", arguments: {source: $s, destination: $d, bare: true}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # Bare repo: refs/objects live at the destination root, no working tree.
  [ -f "$HOME/bare.git/HEAD" ]
  [ ! -d "$HOME/bare.git/.git" ]
  [ ! -f "$HOME/bare.git/file.txt" ]
  [ "$(git -C "$HOME/bare.git" rev-parse --is-bare-repository)" = "true" ]
}

function grit_clone_into_nonempty_dir_surfaces_git_error { # @test
  mkdir -p "$HOME/occupied"
  echo "existing" >"$HOME/occupied/keep.txt"

  local params
  params=$(jq -cn --arg s "$HOME/src" --arg d "$HOME/occupied" \
    '{name: "grit.clone", arguments: {source: $s, destination: $d}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # git refuses to clone into a non-empty dir; its stderr is surfaced.
  local text
  text=$(clone_result_text)
  echo "$text" | grep -qi "already exists and is not an empty directory"
  # The pre-existing file is untouched.
  grep -q "existing" "$HOME/occupied/keep.txt"
}

# ----- clone-perms predicate exits as expected -----
#
# The dynamic-perms gate is consumed by the Claude Code hook layer, which
# does not fire inside the bats sandbox. So — as folio_cwd_guard.bats does
# for folio-perms — invoke the predicate directly to verify its policy.

setup_perms() {
  [ -n "${MOXIN_PATH:-}" ] || skip "MOXIN_PATH not set"
  PERMS="$MOXIN_PATH/grit/bin/clone-perms"
  [ -x "$PERMS" ] || skip "clone-perms not at $PERMS"
}

function grit_clone_perms_allows_destination_in_cwd { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" "$HOME/project/dest"
  [ "$status" -eq 0 ]
}

function grit_clone_perms_allows_relative_destination { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" .tmp/clown
  [ "$status" -eq 0 ]
}

function grit_clone_perms_denies_destination_in_nix_store { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  PWD="$HOME/project" run "$PERMS" /nix/store/foo
  [ "$status" -eq 2 ]
  [[ "$output" == *"immutable"* ]]
}

function grit_clone_perms_asks_destination_outside_allowed_dirs { # @test
  setup_perms
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  HOME=/dev/null CLAUDE_CODE_TMPDIR= PWD="$HOME/project" \
    run "$PERMS" /var/empty/clone
  [ "$status" -eq 1 ]
  [[ "$output" == *"confirmation required"* ]]
}

function grit_clone_perms_allows_destination_in_session_tmpdir { # @test
  setup_perms
  mkdir -p "$HOME/project" "$HOME/session-tmp"
  cd "$HOME/project"

  CLAUDE_CODE_TMPDIR="$HOME/session-tmp" PWD="$HOME/project" \
    run "$PERMS" "$HOME/session-tmp/clone"
  [ "$status" -eq 0 ]
}
