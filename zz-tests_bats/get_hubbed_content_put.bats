#!/usr/bin/env bats

# bats file_tags=get_hubbed

# Tests for get-hubbed.content-put's upsert sha handling. Covers #306: when
# updating an existing file on a non-default branch, the sha lookup must be an
# explicit GET carrying ref=<branch> — `gh api ... -f ref=X` WITHOUT
# `--method GET` silently becomes a POST (gh's documented field behavior),
# the lookup fails, and the PUT goes out with no sha → 422 from GitHub.

load 'common'

BIN="${GET_HUBBED_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/get-hubbed/bin}"

setup() {
  setup_test_home

  mkdir -p "$HOME/bin"
  # gh stub emulating the real `gh api` semantics that matter here:
  #   - `-f`/`-F` fields force POST unless `--method GET` is explicit; the
  #     contents endpoint rejects POST, so such a lookup FAILS (exit 1).
  #   - --method GET + -f ref=X → returns the branch blob sha.
  #   - plain GET (no fields) → returns the default-branch blob sha.
  #   - --method PUT --input - → records the request body for assertions.
  #   - GH_STUB_NO_FILE=1 → lookups 404 (creating a brand-new file).
  # Note: no shebang — the nix sandbox lacks /usr/bin/env.
  cat > "$HOME/bin/gh" <<'EOF'
set -euo pipefail
[ "${1:-}" = "api" ] || { echo "gh stub: unexpected subcommand: $*" >&2; exit 64; }
shift
endpoint="${1:?}"
shift
method=""
ref=""
while [ $# -gt 0 ]; do
  case "$1" in
    --method) method="$2"; shift 2 ;;
    --input) shift 2 ;;
    -f|-F)
      case "$2" in ref=*) ref="${2#ref=}" ;; esac
      shift 2 ;;
    *) shift ;;
  esac
done

if [ "$method" = "PUT" ]; then
  cat > "$HOME/put-body.json"
  printf '%s\n' "$endpoint" > "$HOME/put-endpoint"
  echo '{}'
  exit 0
fi

if [ "${GH_STUB_NO_FILE:-}" = "1" ]; then
  echo '{"message":"Not Found","status":"404"}' >&2
  exit 1
fi

# gh: raw fields switch the request to POST unless --method GET is explicit.
# GitHub rejects POST on the contents endpoint — exactly the #306 failure.
if [ -n "$ref" ] && [ "$method" != "GET" ]; then
  echo '{"message":"Not Found","status":"404"}' >&2
  exit 1
fi

if [ -n "$ref" ]; then
  echo '{"sha":"branch-sha-1234"}'
else
  echo '{"sha":"default-sha-9999"}'
fi
EOF
  chmod +x "$HOME/bin/gh"

  # Prepend so the stub shadows the nix-wrapped gh (suffix pathMode).
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

# Regression for #306: updating an existing file on a feature branch must
# resolve the blob sha ON THAT BRANCH and include it in the PUT body.
function content_put_update_on_branch_includes_branch_sha { # @test
  run "$BIN/content-put" "dir/file.txt" "new content" "update msg" "feature-branch" "owner/repo"
  assert_success

  run cat "$HOME/put-body.json"
  assert_success
  assert_output --partial '"sha": "branch-sha-1234"'
  assert_output --partial '"branch": "feature-branch"'
}

# Updating on the default branch (no branch arg) keeps working.
function content_put_update_default_branch_includes_sha { # @test
  run "$BIN/content-put" "dir/file.txt" "new content" "update msg" "" "owner/repo"
  assert_success

  run cat "$HOME/put-body.json"
  assert_success
  assert_output --partial '"sha": "default-sha-9999"'
  refute_output --partial '"branch"'
}

# Creating a brand-new file sends no sha (GitHub rejects a sha for creates).
function content_put_create_new_file_omits_sha { # @test
  export GH_STUB_NO_FILE=1
  run "$BIN/content-put" "dir/new.txt" "content" "create msg" "feature-branch" "owner/repo"
  assert_success

  run cat "$HOME/put-body.json"
  assert_success
  refute_output --partial '"sha"'
  assert_output --partial '"branch": "feature-branch"'
}
