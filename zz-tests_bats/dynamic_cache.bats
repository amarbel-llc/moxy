#! /usr/bin/env bats

# bats file_tags=native

# Conformance tests for RFC 0001 (dynamic cache-results via result _meta).
# A cache-results = "dynamic" mcp-result tool signals its per-call cache mode
# in _meta."moxy/cache"; moxy resolves it, applies it to result shaping, and
# strips the key. "always" elevates a block to a cached resource (madder:// URI);
# "threshold" caches only oversized output; "never" always stays inline. Caching
# applies whether or not the block carries a mimeType (#8): a mime-bearing block
# keeps its mime on the cached resource, a mime-less one yields a resource with
# no mime. The mime-bearing and mime-LESS fixtures below both exercise this.
#
# Each fixture tool hardcodes a fixed MCP result (no argument threading) so the
# tests exercise the caching feature, not moxin arg wiring.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  write_dyncache_moxin
  mkdir -p "$HOME/project"
  cd "$HOME/project"
}

teardown() {
  teardown_test_home
}

# emit_tool <dir> <name> <mcp-result-json> — write one dynamic-cache fixture
# tool that echoes a fixed MCP result on stdout. The JSON (which contains
# double quotes but no single quotes) is embedded as a TOML single-quoted
# literal string, so no escaping is needed.
emit_tool() {
  local dir="$1" name="$2" json="$3"
  cat >"$dir/$name.toml" <<EOF
schema = 3
result-type = "mcp-result"
cache-results = "dynamic"
command = "echo"
args = ["-n", '$json']
EOF
}

write_dyncache_moxin() {
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/dyn"
  cat >"$moxin_dir/dyn/_moxin.toml" <<'EOF'
schema = 1
name = "dyn"
EOF

  # Blocks carry a mimeType so the mime-gated mcp-result caching path applies.
  local payload='{"content":[{"type":"text","text":"small payload","mimeType":"text/plain"}]'
  emit_tool "$moxin_dir/dyn" always "$payload,\"_meta\":{\"moxy/cache\":\"always\"}}"
  emit_tool "$moxin_dir/dyn" thresh "$payload,\"_meta\":{\"moxy/cache\":\"threshold\"}}"
  emit_tool "$moxin_dir/dyn" never "$payload,\"_meta\":{\"moxy/cache\":\"never\"}}"
  emit_tool "$moxin_dir/dyn" none "$payload}"
  emit_tool "$moxin_dir/dyn" bogus "$payload,\"_meta\":{\"moxy/cache\":\"aggressive\"}}"
  emit_tool "$moxin_dir/dyn" keepmeta "$payload,\"_meta\":{\"moxy/cache\":\"always\",\"other\":\"keep\"}}"

  # Mime-LESS blocks (#8): the block has no mimeType. These exercise the
  # widened caching path that no longer gates on mimeType.
  local nomime_small='{"content":[{"type":"text","text":"small payload"}]'
  emit_tool "$moxin_dir/dyn" nomime_always "$nomime_small,\"_meta\":{\"moxy/cache\":\"always\"}}"
  emit_tool "$moxin_dir/dyn" nomime_thresh "$nomime_small,\"_meta\":{\"moxy/cache\":\"threshold\"}}"

  # A mime-less block whose text is large enough to exceed the token threshold,
  # under threshold policy — proves oversized mime-less output now caches (it
  # used to skip caching entirely because it had no mimeType). ~40k 'x' chars
  # is well over the threshold and well under MAX_ARG_STRLEN for the echo arg.
  local big
  big=$(head -c 40000 </dev/zero | tr '\0' 'x')
  emit_tool "$moxin_dir/dyn" nomime_big \
    "{\"content\":[{\"type\":\"text\",\"text\":\"$big\"}],\"_meta\":{\"moxy/cache\":\"threshold\"}}"

  export MOXIN_PATH="$moxin_dir"
}

# §2: intent "always" → the small payload becomes a cached resource block.
function dynamic_cache_always_caches_small_output { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.always"}'
  assert_success
  echo "$output" | jq -e '.content[0].type == "resource"' || fail "want resource block: $output"
  echo "$output" | jq -e '.content[0].resource.uri | startswith("madder://blobs/")' || fail "want madder URI: $output"
  echo "$output" | jq -e '.content[0].resource.text == "small payload"' || fail "want payload text: $output"
  echo "$output" | jq -e '.content[0].resource.mimeType == "text/plain"' || fail "want mime preserved on cached resource: $output"
}

# §2: intent "threshold" → small output stays plain inline text (no blob).
function dynamic_cache_threshold_leaves_small_output_inline { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.thresh"}'
  assert_success
  echo "$output" | jq -e '.content[0].type == "text"' || fail "want text block: $output"
  echo "$output" | jq -e '.content[0].text == "small payload"' || fail "want payload text: $output"
  # #319: a mime-bearing block under threshold drops the mime (no resource).
  echo "$output" | jq -e '.content[0] | has("mimeType") | not' || fail "mime should be dropped under threshold: $output"
  refute_output --partial "madder://blobs/"
}

# §2: intent "never" → small output stays plain inline text (no blob).
function dynamic_cache_never_leaves_small_output_inline { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.never"}'
  assert_success
  echo "$output" | jq -e '.content[0].type == "text"' || fail "want text block: $output"
  refute_output --partial "madder://blobs/"
}

# §2: moxy MUST strip _meta."moxy/cache" before returning — it never leaks.
function dynamic_cache_strips_meta_key { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.always"}'
  assert_success
  refute_output --partial "moxy/cache"
  echo "$output" | jq -e '(._meta // {}) | has("moxy/cache") | not' || fail "moxy/cache must be stripped: $output"
}

# §2: stripping removes only the cache key; other _meta entries survive.
function dynamic_cache_preserves_other_meta { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.keepmeta"}'
  assert_success
  refute_output --partial "moxy/cache"
  # The always intent still took effect (cached resource)...
  echo "$output" | jq -e '.content[0].type == "resource"' || fail "want resource block: $output"
  # ...and the unrelated _meta key is preserved.
  echo "$output" | jq -e '._meta.other == "keep"' || fail "other _meta must survive: $output"
}

# §3: no intent → threshold fallback (small output stays inline).
function dynamic_cache_absent_intent_falls_back_to_threshold { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.none"}'
  assert_success
  echo "$output" | jq -e '.content[0].type == "text"' || fail "want text block: $output"
  refute_output --partial "madder://blobs/"
}

# §3: an unrecognized intent → threshold fallback, and the call still succeeds.
function dynamic_cache_unrecognized_intent_falls_back_and_succeeds { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.bogus"}'
  assert_success
  echo "$output" | jq -e '.content[0].type == "text"' || fail "want text block (threshold fallback): $output"
  refute_output --partial "madder://blobs/"
  # The unrecognized value is still stripped (it lived under the reserved key).
  refute_output --partial "moxy/cache"
}

# §1: cache-results = "dynamic" under text mode is a config error, so the tool
# is not advertised as callable.
function dynamic_cache_rejected_under_text_mode { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/badcfg"
  cat >"$moxin_dir/badcfg/_moxin.toml" <<'EOF'
schema = 1
name = "badcfg"
EOF
  cat >"$moxin_dir/badcfg/t.toml" <<'EOF'
schema = 3
command = "echo"
result-type = "text"
cache-results = "dynamic"
EOF

  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp_with_stderr "tools/list"
  assert_success
  run jq -e '.tools[] | select(.name == "badcfg.t")' <<<"$output"
  assert_failure
}

# --- #8: caching a MIME-LESS text block ------------------------------------

# always intent on a mime-less block now caches it (previously a no-op because
# the caching path gated on mimeType). Result is a resource block whose
# mimeType is absent (omitempty), not a plain text block.
function dynamic_cache_always_caches_mimeless_block { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.nomime_always"}'
  assert_success
  echo "$output" | jq -e '.content[0].type == "resource"' || fail "want cached resource: $output"
  echo "$output" | jq -e '.content[0].resource.uri | startswith("madder://blobs/")' || fail "want madder URI: $output"
  echo "$output" | jq -e '.content[0].resource.text == "small payload"' || fail "want payload text: $output"
  # No mimeType on the cached resource (the source block had none).
  echo "$output" | jq -e '.content[0].resource | has("mimeType") | not' || fail "mimeType must be absent: $output"
}

# A SMALL mime-less block under threshold stays inline (unchanged behavior).
function dynamic_cache_threshold_small_mimeless_stays_inline { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.nomime_thresh"}'
  assert_success
  echo "$output" | jq -e '.content[0].type == "text"' || fail "want text block: $output"
  echo "$output" | jq -e '.content[0].text == "small payload"' || fail "want payload text: $output"
  refute_output --partial "madder://blobs/"
}

# A LARGE mime-less block under threshold now caches (the #8 fix: previously it
# skipped caching entirely because it had no mimeType, diverging from text mode).
function dynamic_cache_threshold_large_mimeless_caches { # @test
  run_moxy_mcp_v1 "tools/call" '{"name":"dyn.nomime_big"}'
  assert_success
  # Oversized → blob-cached as a resource (or summarized), not returned inline.
  echo "$output" | jq -e '.content[0].type == "resource"' || fail "want cached resource for oversized output: $output"
  echo "$output" | jq -e '.content[0].resource.uri | startswith("madder://blobs/")' || fail "want madder URI: $output"
}
