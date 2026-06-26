#!/usr/bin/env bats

# bats file_tags=just_us_agents

# Regression for #299: a recipe (or a tool it invokes) writing stray text to
# stdout must not corrupt the MCP result. run-recipe is result-type=text, so
# moxy wraps the combined output verbatim — there is no JSON envelope for the
# stray bytes to break. The bug (against an older build where run-recipe owned
# a JSON envelope) surfaced as `invalid MCP result JSON: ... after top-level
# value`. This MUST route through the moxy proxy (run_moxy_mcp_v1) so the
# result-envelope handling is actually exercised — invoking $BIN/run-recipe
# directly bypasses it.

load 'common'

setup() {
  setup_test_home

  # run-recipe relies on an *ambient* `just` (the wrapper bundles none — it
  # runs the user's toolchain against the user's project). Provide a stub
  # that stands in for a recipe writing stray, non-JSON output to stdout —
  # exactly the leak that corrupted the envelope on the old code path. moxy
  # passes its env (incl. PATH) through to the moxin child, so the stub is
  # found. No shebang: the nix sandbox lacks /usr/bin/env (matches the other
  # just-us-agents stubs).
  mkdir -p "$HOME/bin"
  cat >"$HOME/bin/just" <<'STUB'
printf '%s\n' "tsconfig warning: baseUrl is deprecated"
STUB
  chmod +x "$HOME/bin/just"
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

function run_recipe_tolerates_stray_recipe_stdout { # @test
  # No flake.nix, so run-recipe takes the plain `just` branch and hits the
  # stub above.
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "just-us-agents.run-recipe" \
    '{name: $n, arguments: {recipe: "noisy"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # Never the buildMCPResult parse error the bug produced...
  refute_output --partial "invalid MCP result JSON"

  # ...and the stray recipe stdout is carried verbatim in a clean text
  # envelope. (run jq so assert_output prints the result on failure.)
  run jq -r '.content[0].text // .content[0].resource.text // empty' <<<"$output"
  assert_output --partial "baseUrl is deprecated"
}
