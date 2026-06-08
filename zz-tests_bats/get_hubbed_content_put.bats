#!/usr/bin/env bats

# bats file_tags=get_hubbed

# Tests for get-hubbed.content-put's upsert sha handling. Covers #306: when
# updating an existing file on a non-default branch, the sha lookup must be an
# explicit GET carrying ref=<branch> — `gh api ... -f ref=X` WITHOUT
# `--method GET` silently becomes a POST (gh's documented field behavior),
# the lookup fails, and the PUT goes out with no sha → 422 from GitHub.
#
# Also covers #342: content travels via stdin (stdin-param), not argv, so
# files larger than Linux's per-argument exec limit (MAX_ARG_STRLEN, 128 KiB)
# survive both moxy's exec of the script and the script's own jq invocation.
# The script signature is: content-put <path> <message> [branch] [repo] < content

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
  run bash -c 'printf %s "new content" | "$0/content-put" "dir/file.txt" "update msg" "feature-branch" "owner/repo"' "$BIN"
  assert_success

  run cat "$HOME/put-body.json"
  assert_success
  assert_output --partial '"sha": "branch-sha-1234"'
  assert_output --partial '"branch": "feature-branch"'
}

# Updating on the default branch (no branch arg) keeps working.
function content_put_update_default_branch_includes_sha { # @test
  run bash -c 'printf %s "new content" | "$0/content-put" "dir/file.txt" "update msg" "" "owner/repo"' "$BIN"
  assert_success

  run cat "$HOME/put-body.json"
  assert_success
  assert_output --partial '"sha": "default-sha-9999"'
  refute_output --partial '"branch"'
}

# Creating a brand-new file sends no sha (GitHub rejects a sha for creates).
function content_put_create_new_file_omits_sha { # @test
  export GH_STUB_NO_FILE=1
  run bash -c 'printf %s "content" | "$0/content-put" "dir/new.txt" "create msg" "feature-branch" "owner/repo"' "$BIN"
  assert_success

  run cat "$HOME/put-body.json"
  assert_success
  refute_output --partial '"sha"'
  assert_output --partial '"branch": "feature-branch"'
}

# Regression for #342: content larger than MAX_ARG_STRLEN (128 KiB) must
# round-trip byte-identically. Pre-#342 the content rode argv twice (moxy's
# exec of the script, then jq --arg) and either exec died with "Argument
# list too long".
function content_put_large_content_round_trips { # @test
  seq -f 'content line %.0f with some padding to inflate the payload' 1 4000 \
    > "$HOME/big-content.txt"
  # ~230 KB — comfortably past the 128 KiB per-arg limit.
  [ "$(stat -c '%s' "$HOME/big-content.txt")" -gt 131072 ] || fail "fixture too small"

  run bash -c '"$0/content-put" "dir/big.txt" "big msg" "" "owner/repo" < "$1"' \
    "$BIN" "$HOME/big-content.txt"
  assert_success

  jq -r '.content' "$HOME/put-body.json" | base64 -d > "$HOME/round-trip.txt"
  cmp "$HOME/big-content.txt" "$HOME/round-trip.txt" \
    || fail "decoded PUT content differs from input"
}

# Content fidelity: trailing newlines must survive (a $(cat)-style capture
# would strip them and corrupt the committed file).
function content_put_preserves_trailing_newline { # @test
  printf 'line1\nline2\n' > "$HOME/nl-content.txt"

  run bash -c '"$0/content-put" "dir/nl.txt" "nl msg" "" "owner/repo" < "$1"' \
    "$BIN" "$HOME/nl-content.txt"
  assert_success

  jq -r '.content' "$HOME/put-body.json" | base64 -d > "$HOME/nl-round-trip.txt"
  cmp "$HOME/nl-content.txt" "$HOME/nl-round-trip.txt" \
    || fail "trailing newline lost in round-trip"
}

# The stdin-param wiring end-to-end: moxy extracts `content` from the tool
# arguments and delivers it on the script's stdin (never argv).
function content_put_via_moxy_stdin_param { # @test
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  local params='{"name":"get-hubbed.content-put","arguments":{"path":"dir/f.txt","content":"hello stdin","message":"msg","repo_owner_name":"owner/repo"}}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.isError != true' || fail 'content-put returned isError: '"$output"

  run bash -c 'jq -r .content "$HOME/put-body.json" | base64 -d'
  assert_success
  assert_output "hello stdin"
}
