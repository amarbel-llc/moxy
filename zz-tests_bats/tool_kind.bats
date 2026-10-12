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
  echo "$output" | jq -e '[.tools[].name | select(startswith("ring."))] == []' ||
    fail "a moxin with an unknown [annotations] key was served: $output"

  run_moxy status
  assert_output --partial 'bad.toml: unknown key under [annotations]: kind'
}
