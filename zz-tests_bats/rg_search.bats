#! /usr/bin/env bats

# bats file_tags=rg

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

  export XDG_CACHE_HOME="$HOME/.cache"

  # Use the real rg moxin from the source tree.
  # MOXIN_PATH inherited from justfile

  # Create a tree with files of several extensions, all containing a shared
  # marker so a single pattern matches across them.
  mkdir -p "$HOME/tree"
  cd "$HOME/tree"
  printf 'case MARKER\n' >a.sh
  printf 'case MARKER\n' >b.bash
  printf 'case MARKER\n' >c.bats
  printf 'case MARKER\n' >d.txt

  # A match inside a hidden directory — skipped by default, found with hidden.
  mkdir -p .hidden
  printf 'case MARKER\n' >.hidden/h.txt

  # A single very long physical line with the marker embedded among long
  # padding runs — models a wrapped/pretty-printed JSON blob searched as one
  # line (#197). rg emits the whole matching line, so without a per-line cap
  # one match returns the entire (here ~1900-char) line. Kept in its own
  # directory (searched by exact path) so it does not perturb the file-count
  # assertions that sweep $HOME/tree.
  mkdir -p "$HOME/longline"
  {
    printf 'START '
    printf '%*s' 600 '' | tr ' ' A
    printf ' MARKER '
    printf '%*s' 600 '' | tr ' ' B
    printf ' END\n'
  } >"$HOME/longline/longline.txt"
}

teardown() {
  teardown_test_home
}

# Assert the call did not return an MCP error result. The mid-body jq check
# must be load-bearing — bats bodies don't run under set -e, so a bare
# `jq -e` exit status would be discarded; route it through a guard that
# fails the test.
assert_not_iserror() {
  echo "$output" | jq -e '.isError != true' >/dev/null ||
    fail "tool returned isError: $output"
}

# Count the file paths in a files_with_matches result. rg's output is merged
# with stderr (2>&1 in the moxin), so count only path-shaped lines under the
# search tree rather than every non-empty line — a stray rg diagnostic must
# not inflate the count.
result_file_count() {
  echo "$output" | jq -r '.content[0].text' | grep -c "$HOME/tree/"
}

function rg_search_single_glob_matches { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/tree"'","glob":"*.sh"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  assert_equal "$(result_file_count)" 1
  echo "$output" | jq -r '.content[0].text' | grep -q 'a.sh' || fail "a.sh not in results: $output"
}

function rg_search_brace_glob_matches { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/tree"'","glob":"*.{sh,bash,bats}"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  assert_equal "$(result_file_count)" 3
}

# Regression test for #289: a comma-separated glob string must be treated as
# multiple globs (one --glob per element), not a single nonsense pattern that
# silently matches nothing.
function rg_search_comma_separated_glob_matches { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/tree"'","glob":"*.sh,*.bash,*.bats"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  assert_equal "$(result_file_count)" 3
}

# A comma-separated list that includes a brace glob must split only on the
# top-level comma, leaving the brace expansion intact.
function rg_search_comma_with_brace_glob_matches { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/tree"'","glob":"*.txt,*.{sh,bash}"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  # d.txt + a.sh + b.bash = 3
  assert_equal "$(result_file_count)" 3
}

# Empty pieces from leading/trailing/double commas must be skipped without
# tripping `set -e` in the splitter (the script runs with set -euo pipefail).
function rg_search_glob_with_empty_pieces { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/tree"'","glob":",*.sh,,*.bash,"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  # Only a.sh + b.bash; empty pieces are dropped.
  assert_equal "$(result_file_count)" 2
}

# By default ripgrep skips hidden files/dirs, so a match inside .hidden/ is
# silently omitted from a root-level search. See #285.
function rg_search_skips_hidden_by_default { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/tree"'"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  # The four top-level files; .hidden/h.txt is skipped.
  assert_equal "$(result_file_count)" 4
  refute_output --partial ".hidden/h.txt"
}

# Regression test for #285: hidden=true maps to rg --hidden so matches inside
# dot-directories like .github/ are found instead of silently dropped.
function rg_search_hidden_opt_in_finds_dotdirs { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/tree"'","hidden":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  # Four top-level files + .hidden/h.txt = 5.
  assert_equal "$(result_file_count)" 5
  echo "$output" | jq -r '.content[0].text' | grep -q '.hidden/h.txt' ||
    fail ".hidden/h.txt not found with hidden=true: $output"
}

# A genuine no-match (rg exit 1) must remain a non-error success — it must not
# be conflated with the error path (rg exit 2). Guards the #296 fix against
# over-reporting clean no-matches as errors.
function rg_search_no_match_is_clean_success { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"NOSUCHTOKEN_ZZZ","path":"'"$HOME/tree"'"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  assert_equal "$(result_file_count)" 0
}

# Regression test for #296: an rg error (exit 2 — here an invalid regex) must
# surface as isError, not a silent empty success that reads as "no matches".
function rg_search_invalid_regex_is_error { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"(unclosed","path":"'"$HOME/tree"'"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError == true' >/dev/null ||
    fail "invalid regex should surface isError: $output"
  echo "$output" | jq -r '.content[0].text' | grep -qi 'regex' ||
    fail "error text should mention the regex problem: $output"
}

# Regression test for #296: passing a glob-shaped value to `type` (rg exit 2 —
# "unrecognized file type") must surface as isError rather than an empty
# success. This is the issue's "secondary observation".
function rg_search_unrecognized_type_is_error { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/tree"'","type":"*.php"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError == true' >/dev/null ||
    fail "unrecognized type should surface isError: $output"
  echo "$output" | jq -r '.content[0].text' | grep -qi 'file type' ||
    fail "error text should mention the unrecognized file type: $output"
}

# Regression test for #197: in content mode the default per-line cap
# (max_columns=500) truncates a single over-long matching line instead of
# emitting the whole physical line, which on a single-line JSON blob would blow
# past the token budget regardless of match count. The truncated hit stays
# visible via rg's --max-columns-preview marker.
function rg_search_content_caps_long_line_by_default { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/longline/longline.txt"'","output_mode":"content"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  # The raw line is ~1900 chars; capped output must be far shorter and carry
  # the preview marker.
  [ "${#text}" -lt 800 ] || fail "content output not capped (len ${#text}): $output"
  echo "$text" | grep -q 'omitted' || fail "missing --max-columns-preview marker: $output"
}

# Companion to the above: max_columns=0 disables the cap, so the full physical
# line comes back (the escape hatch for callers who genuinely want it).
function rg_search_content_max_columns_zero_disables_cap { # @test
  local params='{"name":"rg.search","arguments":{"pattern":"MARKER","path":"'"$HOME/longline/longline.txt"'","output_mode":"content","max_columns":0}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_not_iserror

  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  # Uncapped: the full ~1900-char line is present (both padding runs intact).
  [ "${#text}" -gt 1200 ] || fail "expected full uncapped line (len ${#text}): $output"
  echo "$text" | grep -q 'omitted' && fail "unexpected preview marker with cap disabled: $output"
  true
}
