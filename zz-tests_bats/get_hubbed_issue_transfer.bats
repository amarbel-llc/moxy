#!/usr/bin/env bats

# bats file_tags=get_hubbed

# Tests get-hubbed.issue-transfer: it resolves the source-issue and
# destination-repo node IDs via one GraphQL query, runs the transferIssue
# mutation, and surfaces the new number/URL. Covers #200.

load 'common'

BIN="${GET_HUBBED_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/get-hubbed/bin}"

setup() {
  setup_test_home

  # gh stub: only handles `gh api graphql`. The id-resolution query and the
  # transferIssue mutation are distinguished by whether the args mention
  # "transferIssue" (only the mutation does).
  mkdir -p "$HOME/bin"
  # No shebang — the nix sandbox lacks /usr/bin/env; bash runs shebang-less
  # scripts as shell scripts (ENOEXEC retry).
  cat >"$HOME/bin/gh" <<'EOF'
set -euo pipefail
if [ "${1:-}" = "api" ] && [ "${2:-}" = "graphql" ]; then
  case "$*" in
  *transferIssue*)
    printf '%s' '{"data":{"transferIssue":{"issue":{"number":7,"url":"https://github.com/dest-org/destrepo/issues/7","repository":{"nameWithOwner":"dest-org/destrepo"}}}}}'
    exit 0
    ;;
  *)
    printf '%s' '{"data":{"src":{"issue":{"id":"I_src","number":283,"url":"https://github.com/src-org/srcrepo/issues/283"}},"dst":{"id":"R_dst","nameWithOwner":"dest-org/destrepo"}}}'
    exit 0
    ;;
  esac
fi
echo "gh-stub: unexpected invocation: $*" >&2
exit 1
EOF
  chmod +x "$HOME/bin/gh"

  # Prepend $HOME/bin so our stub wins over the nix-wrapped gh.
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

# Happy path: transfers and surfaces the old -> new mapping in text mode.
function issue_transfer_reports_old_to_new_mapping { # @test
  # arg-order: number destination_repo repo_owner_name output_format
  run "$BIN/issue-transfer" "283" "dest-org/destrepo" "src-org/srcrepo" ""
  assert_success
  assert_output --partial "src-org/srcrepo#283 → dest-org/destrepo#7"
  assert_output --partial "https://github.com/dest-org/destrepo/issues/7"
}

# json output mode returns the raw transferIssue issue payload. The payload is
# carried as a JSON string inside the MCP text block, so its own quotes are
# escaped — assert on the application/json mimeType and the (unescaped) URL.
function issue_transfer_json_output_returns_payload { # @test
  run "$BIN/issue-transfer" "283" "dest-org/destrepo" "src-org/srcrepo" "json"
  assert_success
  assert_output --partial '"mimeType":"application/json"'
  assert_output --partial 'dest-org/destrepo/issues/7'
}

# Missing destination_repo fails fast with a clear message and no gh call.
function issue_transfer_requires_destination_repo { # @test
  run "$BIN/issue-transfer" "283" "" "src-org/srcrepo" ""
  assert_failure
  assert_output --partial "destination_repo is required"
}

# A non-OWNER/NAME destination is rejected before any gh call.
function issue_transfer_rejects_malformed_destination { # @test
  run "$BIN/issue-transfer" "283" "not-a-repo" "src-org/srcrepo" ""
  assert_failure
  assert_output --partial "OWNER/NAME format"
}
