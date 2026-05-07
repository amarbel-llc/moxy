#! /usr/bin/env bats
#
# folio's CWD guard was removed in favor of dynamic-perms (see
# bin/folio-perms). The native tools no longer reject paths outside CWD —
# in tests where Claude Code's hook layer doesn't fire, paths anywhere on
# the filesystem are accessible. This file invokes the bin/folio-perms
# predicate directly to verify its decision policy.

setup_file() {
  # Extract the release tarball once so the predicate test can invoke
  # bin/folio-perms via the wrapped script (with PATH baked in).
  if [ -n "${RELEASE_TARBALL_DIR:-}" ] && [ -d "$RELEASE_TARBALL_DIR" ]; then
    for candidate in "$RELEASE_TARBALL_DIR"/moxy-*.tar.gz; do
      [ -f "$candidate" ] && RELEASE_TARBALL="$candidate" && break
    done
    if [ -f "${RELEASE_TARBALL:-}" ]; then
      export RELEASE_EXTRACT
      RELEASE_EXTRACT=$(mktemp -d)
      tar -xzf "$RELEASE_TARBALL" -C "$RELEASE_EXTRACT"
    fi
  fi
}

teardown_file() {
  [ -n "${RELEASE_EXTRACT:-}" ] && rm -rf "$RELEASE_EXTRACT"
}

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

# ----- folio-perms predicate exits as expected -----
#
# These tests invoke the bin/folio-perms script directly to verify its
# decision policy. The script lives inside the nix-built moxin tree.

setup_perms() {
  [ -n "${RELEASE_EXTRACT:-}" ] || skip "release tarball not extracted"
  PERMS="$RELEASE_EXTRACT/moxy/share/moxy/moxins/folio/bin/folio-perms"
  [ -x "$PERMS" ] || skip "folio-perms not in extracted tree"
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
