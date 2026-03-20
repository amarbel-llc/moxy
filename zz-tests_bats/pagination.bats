#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  FIXTURES_DIR="$(cd "$(dirname "$BATS_TEST_FILE")/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

function pagination_returns_full_response_without_params { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items"}'
  assert_success
  echo "$output" | jq -e '.contents[0].text == "[1,2,3,4,5,6,7,8,9,10]"'
}

function pagination_slices_with_offset_and_limit { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=0&limit=3"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.total == 10'
  echo "$text" | jq -e '.offset == 0'
  echo "$text" | jq -e '.limit == 3'
  echo "$text" | jq -e '.items == [1,2,3]'
}

function pagination_second_page { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=3&limit=3"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.items == [4,5,6]'
  echo "$text" | jq -e '.total == 10'
}

function pagination_last_page_partial { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=8&limit=5"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.items == [9,10]'
  echo "$text" | jq -e '.total == 10'
}

function pagination_disabled_passes_through { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=0&limit=3"}'
  assert_success
  # Without paginate=true, query params are forwarded as-is (server ignores them)
  # and the full array is returned
  echo "$output" | jq -e '.contents[0].text == "[1,2,3,4,5,6,7,8,9,10]"'
}

function pagination_default_limit { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=0"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.limit == 50'
  # All 10 items returned since 10 < 50
  echo "$text" | jq -e '.items == [1,2,3,4,5,6,7,8,9,10]'
}
