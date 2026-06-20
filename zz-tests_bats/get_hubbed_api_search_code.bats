#!/usr/bin/env bats

# bats file_tags=get_hubbed

# get-hubbed.api must reject /search/code: GitHub code search returns
# authoritative-looking false negatives, so the adhoc client refuses it (the
# content-search tool was removed entirely). Metadata search (/search/issues,
# /search/repositories, …) is unaffected. Covers #373.

load 'common'

BIN="${GET_HUBBED_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/get-hubbed/bin}"

GH_STUB=""

setup() {
  setup_test_home

  GH_STUB="$HOME/bin/gh"
  mkdir -p "$HOME/bin"
  # Shebang-less stub (the nix sandbox lacks /usr/bin/env); records argv to
  # $HOME/gh-argv so a test can prove whether gh was invoked at all.
  cat >"$GH_STUB" <<'EOF'
set -euo pipefail
cat >/dev/null 2>&1 || true
printf '%s\n' "$@" > "$HOME/gh-argv"
echo '{}'
EOF
  chmod +x "$GH_STUB"
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

# A bare /search/code path is rejected before gh is ever called.
function api_rejects_search_code_path { # @test
  run "$BIN/api" "GET" "/search/code" "" "" "" ""
  assert_failure
  assert_output --partial "/search/code is disabled"
  assert_output --partial "content-tree"
  [ ! -f "$HOME/gh-argv" ] || fail "gh should not be invoked for /search/code"
}

# A query string on /search/code is still rejected.
function api_rejects_search_code_with_query { # @test
  run "$BIN/api" "GET" "/search/code?q=foo+repo:owner/name" "" "" "" ""
  assert_failure
  assert_output --partial "disabled"
  [ ! -f "$HOME/gh-argv" ] || fail "gh should not be invoked for /search/code"
}

# A full api.github.com URL form is rejected too.
function api_rejects_search_code_full_url { # @test
  run "$BIN/api" "GET" "https://api.github.com/search/code?q=foo" "" "" "" ""
  assert_failure
  assert_output --partial "disabled"
  [ ! -f "$HOME/gh-argv" ] || fail "gh should not be invoked for /search/code"
}

# Metadata search is NOT code search — /search/issues passes through to gh.
function api_allows_search_issues { # @test
  run "$BIN/api" "GET" "/search/issues?q=repo:owner/name+is:open" "" "" "" ""
  assert_success
  run cat "$HOME/gh-argv"
  assert_success
  assert_output --partial "/search/issues"
}
