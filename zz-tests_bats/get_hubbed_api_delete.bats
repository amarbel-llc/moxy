#!/usr/bin/env bats

# bats file_tags=get_hubbed

# Tests that get-hubbed.api passes --method DELETE (and the correct endpoint)
# to `gh api` when called with method=DELETE. Covers #214.

load 'common'

BIN="${GET_HUBBED_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/get-hubbed/bin}"

# A gh stub that records its full argv to $HOME/gh-argv, then returns canned
# JSON so the caller sees a valid non-empty response.
GH_STUB=""

setup() {
  setup_test_home

  GH_STUB="$HOME/bin/gh"
  mkdir -p "$HOME/bin"
  # Note: no shebang line. The nix sandbox lacks /usr/bin/env, so
  # #!/usr/bin/env bash would fail. Bash falls back to executing
  # shebang-less scripts as shell scripts (ENOEXEC → shell retry).
  cat > "$GH_STUB" <<'EOF'
# Minimal gh stub for api-delete tests. Records argv to $HOME/gh-argv
# and returns canned JSON so the bin script sees a successful response.
set -euo pipefail
printf '%s\n' "$@" > "$HOME/gh-argv"
echo '{}'
EOF
  chmod +x "$GH_STUB"

  # Prepend $HOME/bin so our stub wins over the nix-wrapped gh.
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

# api DELETE: gh is called with --method DELETE and the correct endpoint.
@test "api DELETE: passes --method DELETE and endpoint to gh api" {
  run "$BIN/api" "DELETE" "/repos/owner/repo/issues/comments/42" "" "" "" ""
  assert_success
  # The stub records one arg per line.
  run cat "$HOME/gh-argv"
  assert_success
  assert_output --partial "api"
  assert_output --partial "--method"
  assert_output --partial "DELETE"
  assert_output --partial "/repos/owner/repo/issues/comments/42"
}

# api DELETE: method comes through as DELETE, not any other verb.
@test "api DELETE: method is DELETE not GET or POST" {
  run "$BIN/api" "DELETE" "/repos/owner/repo/git/refs/heads/old-branch" "" "" "" ""
  assert_success
  run cat "$HOME/gh-argv"
  assert_success
  assert_output --partial "DELETE"
  refute_output --partial "GET"
  refute_output --partial "POST"
}

# api DELETE: endpoint is forwarded verbatim (no mangling).
@test "api DELETE: endpoint is forwarded verbatim to gh api" {
  local endpoint="/repos/etsy/my-repo/releases/99"
  run "$BIN/api" "DELETE" "$endpoint" "" "" "" ""
  assert_success
  run cat "$HOME/gh-argv"
  assert_success
  assert_output --partial "$endpoint"
}

# api DELETE with optional body: body is piped to gh via stdin.
# The stub exits 0 and the bin script must not error when body is set.
@test "api DELETE: body is accepted without error" {
  run "$BIN/api" "DELETE" "/repos/owner/repo/labels/12" '{"reason":"cleanup"}' "" "" ""
  assert_success
}

# api DELETE: explicit paginate=false (falsy) does not add --paginate flag.
@test "api DELETE: paginate=false does not add --paginate to gh args" {
  run "$BIN/api" "DELETE" "/repos/owner/repo/issues/99" "" "" "" "false"
  assert_success
  run cat "$HOME/gh-argv"
  assert_success
  refute_output --partial "--paginate"
}
