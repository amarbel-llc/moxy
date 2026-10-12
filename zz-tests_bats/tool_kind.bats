#! /usr/bin/env bats

# bats file_tags=tool_kind

# A tool's `kind` on tools/list (#451): a moxin's top-level `kind` is emitted
# as `_meta.kind`, a child server's own `_meta` passes through whole, and a
# key the [annotations] table has no field for stops the moxin from loading.
# See moxin(7) TOOL KIND. The streamable-http case is in tool_kind_http.bats.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
  # The lane's own MOXIN_PATH is every moxin this repo builds.
  SHIPPED_MOXIN_PATH="${MOXIN_PATH:-}"
  export MOXIN_PATH="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$MOXIN_PATH/ring" "$HOME/repo"
  cat >"$MOXIN_PATH/ring/_moxin.toml" <<'EOF'
schema = 1
name = "ring"
EOF
  cd "$HOME/repo"
}

teardown() {
  teardown_test_home
}

write_ring_tool() {
  cat >"$MOXIN_PATH/ring/$1.toml" <<EOF
schema = 3
description = "$1"
command = "echo"
$2
EOF
}

# The `_meta` of one listed tool, or the JSON literal null when it has none.
tool_meta() {
  echo "$output" | jq -c --arg n "$1" '.tools[] | select(.name == $n) | ._meta // null'
}

ring_tool_names() {
  echo "$output" | jq -c '[.tools[].name | select(startswith("ring."))] | sort'
}

function a_moxin_kind_is_listed_as_meta_kind { # @test
  write_ring_tool list-issues 'kind = "list"'
  write_ring_tool create-issue 'kind = "issue"'
  write_ring_tool plain ''

  run_moxy_mcp_v1 tools/list
  assert_success
  assert_equal "$(tool_meta ring.list-issues)" '{"kind":"list"}'
  assert_equal "$(tool_meta ring.create-issue)" '{"kind":"issue"}'
  assert_equal "$(tool_meta ring.plain)" 'null'
}

function a_kind_is_carried_as_written_not_checked_against_a_vocabulary { # @test
  write_ring_tool odd 'kind = "Not A Known Kind"'

  run_moxy_mcp_v1 tools/list
  assert_success
  assert_equal "$(tool_meta ring.odd)" '{"kind":"Not A Known Kind"}'
}

function a_child_servers_meta_passes_through_whole { # @test
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/meta-tool-server.bash"]
EOF

  run_moxy_mcp_v1 tools/list
  assert_success
  assert_equal "$(tool_meta srv.create-issue | jq -cS .)" \
    '{"flag":true,"kind":"issue","vendor/extra":{"n":1,"tags":["a","b"]}}'
  assert_equal "$(tool_meta srv.plain)" 'null'
}

function an_ephemeral_child_servers_meta_passes_through_whole { # @test
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/meta-tool-server.bash"]
ephemeral = true
EOF

  run_moxy_mcp_v1 tools/list
  assert_success
  assert_equal "$(tool_meta srv.create-issue | jq -cS .)" \
    '{"flag":true,"kind":"issue","vendor/extra":{"n":1,"tags":["a","b"]}}'
}

function an_unknown_key_under_annotations_stops_the_moxin_from_loading { # @test
  write_ring_tool good 'kind = "list"'
  write_ring_tool bad '
[annotations]
read-only-hint = true
kind = "list"'

  run_moxy_mcp_v1 tools/list
  assert_success
  # None of its tools, the good one included; in their place the status tool
  # of a failed server, which says why.
  assert_equal "$(ring_tool_names)" '["ring.status"]'
  echo "$output" | jq -e '.tools[] | select(.name == "ring.status") | .description | test("unknown key under \\[annotations\\]: kind")' ||
    fail "ring.status does not say why the moxin was refused: $output"

  run_moxy status
  assert_output --partial 'bad.toml: unknown key under [annotations]: kind'
}

# A refused moxin must not be replaced by the same-named one further down
# MOXIN_PATH: that would serve other tools, at other permission tiers, under
# its name.
function a_refused_moxin_is_not_replaced_by_a_same_named_one_lower_in_the_path { # @test
  write_ring_tool override-tool '
[annotations]
readOnlyHint = true'

  local lower="$BATS_TEST_TMPDIR/lower"
  mkdir -p "$lower/ring"
  cp "$MOXIN_PATH/ring/_moxin.toml" "$lower/ring/_moxin.toml"
  cat >"$lower/ring/bundled-tool.toml" <<'EOF'
schema = 3
description = "the lower-priority ring"
command = "echo"
EOF
  export MOXIN_PATH="$MOXIN_PATH:$lower"

  run_moxy_mcp_v1 tools/list
  assert_success
  assert_equal "$(ring_tool_names)" '["ring.status"]'

  run_moxy status
  assert_output --partial 'unknown key under [annotations]: readOnlyHint'
  assert_output --partial "[shadowed by $BATS_TEST_TMPDIR/moxins]"
}

# The refusal is stricter than what used to load, so everything this repo
# builds has to pass it already. Run against the built moxins, with their
# placeholders substituted, which is what users get.
function every_moxin_this_repo_builds_still_loads { # @test
  [[ -n $SHIPPED_MOXIN_PATH ]] || skip "no lane MOXIN_PATH; run through the nix bats lane"
  export MOXIN_PATH="$SHIPPED_MOXIN_PATH"

  run_moxy status
  refute_output --partial 'FAILED:'
  refute_output --partial 'unknown key under [annotations]'
  # Not vacuous: the path really holds moxins.
  assert_output --regexp 'Merged: [1-9][0-9]* moxin\(s\)'
}
