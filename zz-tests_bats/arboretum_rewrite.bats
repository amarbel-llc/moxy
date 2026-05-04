#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function arboretum_rewrite_dry_run_emits_diff_without_modifying_file { # @test
  cat > "$HOME/app.js" <<'EOF'
console.log("startup");
EOF

  local before
  before=$(cat "$HOME/app.js")

  local params
  params=$(jq -cn \
    --arg n "arboretum.rewrite" \
    --arg pat "console.log(\$MSG)" \
    --arg rep "logger.info(\$MSG)" \
    --arg path "$HOME" \
    '{name: $n, arguments: {pattern: $pat, rewrite: $rep, path: $path, dry_run: true}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text // .content[0].text')

  # Diff appears in the output (ast-grep diff format includes a "│" separator).
  echo "$text" | grep -qE 'logger\.info\("startup"\)'

  # File on disk is unchanged.
  local after
  after=$(cat "$HOME/app.js")
  [ "$before" = "$after" ]
}

function arboretum_rewrite_apply_modifies_the_file { # @test
  cat > "$HOME/app.js" <<'EOF'
console.log("startup");
console.log("done");
EOF

  local params
  params=$(jq -cn \
    --arg n "arboretum.rewrite" \
    --arg pat "console.log(\$MSG)" \
    --arg rep "logger.info(\$MSG)" \
    --arg path "$HOME" \
    '{name: $n, arguments: {pattern: $pat, rewrite: $rep, path: $path}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # File on disk now has the rewrite applied.
  grep -q 'logger.info("startup")' "$HOME/app.js"
  grep -q 'logger.info("done")' "$HOME/app.js"
  ! grep -q 'console.log' "$HOME/app.js"
}
