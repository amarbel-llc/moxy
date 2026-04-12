#! /usr/bin/env bats
#
# Tests that V0 and V1 clients receive valid content blocks.
# Reproduces: invalid_union Zod failures in Claude Code when moxy
# returns content blocks that violate the client's schema.
#
# V0 invariant: every content block must have "text" as a string
# (not null, not missing). No type="resource" blocks (must be flattened).
#
# V1 invariant: type="text" blocks must have "text" as a string.
# type="resource" blocks must have a "resource" sub-object.
#
# Key regression: empty text + mimeType on schema=2 MCP results.
# The mimeType rewrite strips mimeType but leaves Text="", which
# V1's omitempty drops, producing {"type":"text"} with no text field.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output

  export XDG_CACHE_HOME="$HOME/.cache"
}

teardown() {
  teardown_test_home
}

# ---------------------------------------------------------------------------
# V0 content-block validators
# ---------------------------------------------------------------------------

# Assert every content block has "text" as a string (Claude Code Zod requirement).
# V0 ContentBlockV0.Text is json:"text" (no omitempty), so it must always be present.
assert_v0_text_field_present() {
  local missing
  missing=$(echo "$output" | jq '[.content // [] | .[] | select(.text == null or (.text | type) != "string")] | length')
  if [[ "$missing" -ne 0 ]]; then
    echo "Found $missing content block(s) with missing or non-string 'text' field" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

# Assert no content blocks have type="resource" — the V0 downgrade
# should convert all resource blocks to text blocks.
assert_v0_no_resource_type() {
  local count
  count=$(echo "$output" | jq '[.content // [] | .[] | select(.type == "resource")] | length')
  if [[ "$count" -ne 0 ]]; then
    echo "Found $count content block(s) with type='resource' — V0 downgrade should flatten these to 'text'" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

# Assert no text blocks have mimeType set (MCP spec violation).
assert_no_mimetype_on_text_blocks() {
  local count
  count=$(echo "$output" | jq '[.content // [] | .[] | select(.type == "text" and .mimeType != null and .mimeType != "")] | length')
  if [[ "$count" -ne 0 ]]; then
    echo "Found $count text block(s) with mimeType set — violates MCP spec" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

# Combined V0 validity check: all three assertions.
assert_valid_v0_content() {
  assert_v0_text_field_present
  assert_v0_no_resource_type
  assert_no_mimetype_on_text_blocks
}

# ---------------------------------------------------------------------------
# V1 content-block validators
# ---------------------------------------------------------------------------

# Assert V1 resource blocks have a non-null resource sub-object.
# Claude Code V1 Zod: type="resource" requires resource: { uri, text|blob }.
assert_v1_resource_blocks_valid() {
  local malformed
  malformed=$(echo "$output" | jq '[.content // [] | .[] | select(.type == "resource" and .resource == null)] | length')
  if [[ "$malformed" -ne 0 ]]; then
    echo "Found $malformed resource block(s) missing the 'resource' sub-object" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

# Assert V1 text blocks have non-null text field.
# Claude Code V1 Zod: type="text" requires text: string.
assert_v1_text_blocks_valid() {
  local malformed
  malformed=$(echo "$output" | jq '[.content // [] | .[] | select(.type == "text" and (.text == null or (.text | type) != "string"))] | length')
  if [[ "$malformed" -ne 0 ]]; then
    echo "Found $malformed text block(s) with missing or non-string 'text' field" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

# Combined V1 validity check.
assert_valid_v1_content() {
  assert_v1_resource_blocks_valid
  assert_v1_text_blocks_valid
}

# ===========================================================================
# Schema 1 with content-type → V0 downgrade
# ===========================================================================

function v0_schema1_content_type_downgrades_to_text { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/typed"
  cat >"$moxin_dir/typed/_moxin.toml" <<'EOF'
schema = 1
name = "typed"
EOF
  cat >"$moxin_dir/typed/api.toml" <<'EOF'
schema = 1
command = "echo"
args = ["-n", "{\"ok\":true}"]
content-type = "application/json"
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"typed.api"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_valid_v0_content
  # The text content should contain the original output.
  echo "$output" | jq -e '.content[0].text | contains("{\"ok\":true}")'
}

# ===========================================================================
# Schema 2 MCP result with mimeType → V0 downgrade
# ===========================================================================

function v0_schema2_mcp_result_mimetype_downgrades_to_text { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/s2"
  cat >"$moxin_dir/s2/_moxin.toml" <<'EOF'
schema = 1
name = "s2"
EOF
  cat >"$moxin_dir/s2/api.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "{\"content\":[{\"type\":\"text\",\"text\":\"hello from mcp\",\"mimeType\":\"text/plain\"}]}"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"s2.api"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_valid_v0_content
  echo "$output" | jq -e '.content[0].text == "hello from mcp"'
}

# ===========================================================================
# Schema 2 text mode with content-type → V0 downgrade
# ===========================================================================

function v0_schema2_text_mode_content_type_downgrades_to_text { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/s2text"
  cat >"$moxin_dir/s2text/_moxin.toml" <<'EOF'
schema = 1
name = "s2text"
EOF
  cat >"$moxin_dir/s2text/plain.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "just plain text"]
result-type = "text"
content-type = "text/csv"
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"s2text.plain"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_valid_v0_content
  echo "$output" | jq -e '.content[0].text == "just plain text"'
}

# ===========================================================================
# Schema 2 MCP result with application/json mimeType → V0 downgrade
# Mirrors the get-hubbed issue-list pattern: inline bash that constructs
# MCP JSON with mimeType on the text block.
# ===========================================================================

function v0_schema2_mcp_json_mimetype_downgrades_to_text { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/gh"
  cat >"$moxin_dir/gh/_moxin.toml" <<'EOF'
schema = 1
name = "gh"
EOF
  # Simulates the get-hubbed issue-list pattern: bash script builds MCP JSON
  # with mimeType on the text block.
  cat >"$moxin_dir/gh/issue-list.toml" <<'TOML'
schema = 2
command = "bash"
args = ["-c", """
text='[{"number":1,"title":"test issue"}]'
jq -cn --arg text "$text" --arg mime "application/json" \
  '{content:[{type:"text",text:$text,mimeType:$mime}]}'
"""]
TOML

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"gh.issue-list"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_valid_v0_content
}

# ===========================================================================
# V1 path: ensure resource blocks are well-formed for V1 clients too
# ===========================================================================

function v1_schema2_mcp_result_resource_block_valid { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/s2"
  cat >"$moxin_dir/s2/_moxin.toml" <<'EOF'
schema = 1
name = "s2"
EOF
  cat >"$moxin_dir/s2/api.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "{\"content\":[{\"type\":\"text\",\"text\":\"hello\",\"mimeType\":\"text/plain\"}]}"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"s2.api"}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_valid_v1_content
  echo "$output" | jq -e '.content[0].type == "resource"'
  echo "$output" | jq -e '.content[0].resource.text == "hello"'
  echo "$output" | jq -e '.content[0].resource.mimeType == "text/plain"'
}

function v1_schema2_json_mimetype_resource_block_valid { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/gh"
  cat >"$moxin_dir/gh/_moxin.toml" <<'EOF'
schema = 1
name = "gh"
EOF
  cat >"$moxin_dir/gh/issue-list.toml" <<'TOML'
schema = 2
command = "bash"
args = ["-c", """
text='[{"number":1,"title":"test issue"}]'
jq -cn --arg text "$text" --arg mime "application/json" \
  '{content:[{type:"text",text:$text,mimeType:$mime}]}'
"""]
TOML

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"gh.issue-list"}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_valid_v1_content
  echo "$output" | jq -e '.content[0].type == "resource"'
  echo "$output" | jq -e '.content[0].resource != null'
  echo "$output" | jq -e '.content[0].resource.mimeType == "application/json"'
}

# ===========================================================================
# V0 downgrade with multi-block content (text + resource mix)
# ===========================================================================

function v0_multi_block_mixed_types_all_valid { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/multi"
  cat >"$moxin_dir/multi/_moxin.toml" <<'EOF'
schema = 1
name = "multi"
EOF
  # Returns two content blocks: one plain text, one text-with-mimeType.
  # The mimeType block gets rewritten to a resource block, then both
  # must be validly downgraded for V0.
  cat >"$moxin_dir/multi/mix.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "{\"content\":[{\"type\":\"text\",\"text\":\"summary\"},{\"type\":\"text\",\"text\":\"{\\\"data\\\":1}\",\"mimeType\":\"application/json\"}]}"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"multi.mix"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length == 2'
  assert_valid_v0_content
  # First block: plain text, unchanged.
  echo "$output" | jq -e '.content[0].text == "summary"'
  # Second block: was text+mimeType, should be downgraded to text.
  echo "$output" | jq -e '.content[1].text == "{\"data\":1}"'
}

# ===========================================================================
# V0 downgrade of grit.diff-like tool (real moxin, if available)
# Mirrors grit_diff.bats but adds the stricter V0 validators.
# ===========================================================================

# ===========================================================================
# REGRESSION: empty text + mimeType → V1 omitempty drops text field
# This is the exact pattern that causes the invalid_union Zod failures
# in Claude Code. When a schema=2 moxin returns:
#   {content:[{type:"text", text:"", mimeType:"text/plain"}]}
# buildMCPResult strips the mimeType (text is empty, can't make resource
# block), leaving ContentBlockV1{Type:"text", Text:""}, which V1's
# json:"text,omitempty" omits, producing {"type":"text"} on the wire.
# ===========================================================================

function v1_empty_text_with_mimetype_dropped { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/empty"
  cat >"$moxin_dir/empty/_moxin.toml" <<'EOF'
schema = 1
name = "empty"
EOF
  # Returns a text block with mimeType but EMPTY text.
  # This is what freud-messages returns when no messages match.
  cat >"$moxin_dir/empty/search.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "{\"content\":[{\"type\":\"text\",\"text\":\"\",\"mimeType\":\"text/plain\"}]}"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"empty.search"}'

  # V1 client: empty text block with mimeType is dropped entirely
  # to avoid V1 omitempty producing {"type":"text"} (no text field).
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  assert_valid_v1_content
  echo "$output" | jq -e '(.content | length) == 0'
}

function v0_empty_text_with_mimetype_dropped { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/empty"
  cat >"$moxin_dir/empty/_moxin.toml" <<'EOF'
schema = 1
name = "empty"
EOF
  cat >"$moxin_dir/empty/search.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "{\"content\":[{\"type\":\"text\",\"text\":\"\",\"mimeType\":\"text/plain\"}]}"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"empty.search"}'

  # V0 client: same behavior — empty text block with mimeType is dropped.
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_valid_v0_content
  echo "$output" | jq -e '(.content | length) == 0'
}

function v1_empty_text_with_json_mimetype_still_valid { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/gh"
  cat >"$moxin_dir/gh/_moxin.toml" <<'EOF'
schema = 1
name = "gh"
EOF
  # Simulates issue-list returning no results: empty text with application/json mimeType.
  cat >"$moxin_dir/gh/issue-list.toml" <<'TOML'
schema = 2
command = "bash"
args = ["-c", """
jq -cn --arg text '' --arg mime 'application/json' \
  '{content:[{type:"text",text:$text,mimeType:$mime}]}'
"""]
TOML

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"gh.issue-list"}'

  # V1 client: must produce a valid content block.
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  assert_valid_v1_content
}

# ===========================================================================
# V0 downgrade of grit.diff-like tool (real moxin, if available)
# Mirrors grit_diff.bats but adds the stricter V0 validators.
# ===========================================================================

function v0_grit_diff_content_blocks_claude_compatible { # @test
  # Use the real grit moxin from the source tree.
  export MOXIN_PATH="$BATS_TEST_DIRNAME/../moxins"

  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  git init
  git config user.email "test@test.com"
  git config user.name "Test"

  echo "original" > file.txt
  git add file.txt
  git commit -m "initial"
  echo "modified" > file.txt

  local params='{"name":"grit.diff"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_valid_v0_content
}
