#!/usr/bin/env bats

# bats file_tags=get_hubbed

# Tests that get-hubbed bin scripts resolve the repo from `origin` rather than
# from gh's default remote resolution (which prefers `upstream` in dual-remote
# clones). Covers #220.

load 'common'

BIN="${GET_HUBBED_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/get-hubbed/bin}"

# A minimal gh stub that records the -R argument and exits 0 (printing nothing
# useful — these tests only care WHICH repo is targeted, not the response).
# The stub also handles `gh api /user` (returns the authenticated user) and
# `gh repo view` (simulates upstream resolution by returning upstream org/name).
GH_STUB=""

setup() {
  setup_test_home

  # Create a git repo with two remotes: origin=fork, upstream=canonical.
  REPO="$HOME/work"
  git init -q -b main "$REPO"
  cd "$REPO"
  git config user.email t@t
  git config user.name t
  git config commit.gpgSign false
  git remote add origin "git@github.com:fork-org/myrepo.git"
  git remote add upstream "https://github.com/canonical-org/myrepo.git"
  git commit --allow-empty -m base -q

  # Write a gh stub that:
  #   - `gh api /user --jq .login`  → "fork-user"
  #   - `gh repo view ...`          → simulates gh picking upstream → "canonical-org/myrepo"
  #   - `gh issue create -R <repo>` → prints "Resolved-repo: <repo>" so the test
  #     can assert which repo was targeted.
  #   - Any other invocation        → delegates to the real gh (needed for
  #     tests that don't exercise the dual-remote path).
  GH_STUB="$HOME/bin/gh"
  mkdir -p "$HOME/bin"
  # Note: no shebang line. The nix sandbox lacks /usr/bin/env, so
  # #!/usr/bin/env bash would fail. Bash falls back to executing
  # shebang-less scripts as shell scripts (ENOEXEC → shell retry).
  cat > "$GH_STUB" <<'EOF'
# Minimal gh stub for dual-remote tests.
set -euo pipefail

if [ "${1:-}" = "api" ] && [ "${2:-}" = "/user" ]; then
  # Return authenticated user login.
  echo "fork-user"
  exit 0
fi

if [ "${1:-}" = "repo" ] && [ "${2:-}" = "view" ]; then
  # Simulate gh picking upstream: return upstream repo name.
  # A real gh would return "myrepo" for --json name --jq .name,
  # but the owner would resolve to canonical-org via gh's context.
  # Here we return the upstream name to prove that if gh-resolution
  # were used, the wrong repo would be targeted.
  echo "canonical-org/myrepo"
  exit 0
fi

# issue create / issue close / issue comment etc: capture the -R argument.
# Walk args looking for -R <value>.
repo=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-R" ]; then
    repo="$arg"
    break
  fi
  prev="$arg"
done

if [ -n "$repo" ]; then
  echo "Resolved-repo: $repo"
  exit 0
fi

# No -R found — shouldn't happen in our test calls; fail loudly.
echo "gh-stub: no -R found in: $*" >&2
exit 1
EOF
  chmod +x "$GH_STUB"

  # Prepend $HOME/bin so our stub wins over the nix-wrapped gh.
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

# issue-create: should resolve to origin (fork-org/myrepo), not upstream.
function issue_create_resolves_to_origin_in_dual_remote_clone { # @test
  cd "$REPO"
  # arg-order: title body labels_json repo_owner_name
  run "$BIN/issue-create" "test title" "" "" ""
  assert_success
  assert_output --partial "Resolved-repo: fork-org/myrepo"
  refute_output --partial "canonical-org"
}

# issue-create: explicit repo_owner_name overrides origin resolution.
function issue_create_explicit_repo_owner_name_is_respected { # @test
  cd "$REPO"
  run "$BIN/issue-create" "test title" "" "" "explicit-org/explicit-repo"
  assert_success
  assert_output --partial "Resolved-repo: explicit-org/explicit-repo"
}

# issue-close: should resolve to origin in dual-remote clone.
function issue_close_resolves_to_origin_in_dual_remote_clone { # @test
  cd "$REPO"
  # arg-order: number comment reason repo_owner_name
  run "$BIN/issue-close" "42" "" "" ""
  assert_success
  assert_output --partial "Resolved-repo: fork-org/myrepo"
  refute_output --partial "canonical-org"
}

# issue-comment: should resolve to origin in dual-remote clone.
function issue_comment_resolves_to_origin_in_dual_remote_clone { # @test
  cd "$REPO"
  # arg-order: number body repo_owner_name
  run "$BIN/issue-comment" "42" "hello" ""
  assert_success
  assert_output --partial "Resolved-repo: fork-org/myrepo"
  refute_output --partial "canonical-org"
}

# pr-create: should resolve to origin in dual-remote clone.
function pr_create_resolves_to_origin_in_dual_remote_clone { # @test
  cd "$REPO"
  # arg-order: title body base head draft labels_json repo_owner_name
  run "$BIN/pr-create" "test PR" "" "" "" "" "" ""
  assert_success
  assert_output --partial "Resolved-repo: fork-org/myrepo"
  refute_output --partial "canonical-org"
}

# Verify single-remote repos still work (no origin resolution regression).
function issue_create_works_in_single_remote_clone_only_origin { # @test
  SINGLE="$HOME/single"
  git init -q -b main "$SINGLE"
  cd "$SINGLE"
  git config user.email t@t
  git config user.name t
  git config commit.gpgSign false
  git remote add origin "git@github.com:solo-org/solo-repo.git"
  git commit --allow-empty -m base -q
  # arg-order: title body labels_json repo_owner_name
  run "$BIN/issue-create" "test title" "" "" ""
  assert_success
  assert_output --partial "Resolved-repo: solo-org/solo-repo"
}

# Verify HTTPS remote URLs are parsed correctly.
function issue_create_resolves_to_origin_with_HTTPS_remote_URL { # @test
  HTTPS_REPO="$HOME/https-work"
  git init -q -b main "$HTTPS_REPO"
  cd "$HTTPS_REPO"
  git config user.email t@t
  git config user.name t
  git config commit.gpgSign false
  git remote add origin "https://github.com/https-org/https-repo.git"
  git remote add upstream "https://github.com/canonical-org/myrepo.git"
  git commit --allow-empty -m base -q
  # arg-order: title body labels_json repo_owner_name
  run "$BIN/issue-create" "test title" "" "" ""
  assert_success
  assert_output --partial "Resolved-repo: https-org/https-repo"
  refute_output --partial "canonical-org"
}
