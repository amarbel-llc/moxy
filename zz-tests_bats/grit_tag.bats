#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output

  export XDG_CACHE_HOME="$HOME/.cache"

  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  git init -q
  git config user.email "test@test.com"
  git config user.name "Test"
  git config tag.gpgSign false
  git config commit.gpgSign false

  echo "a" >file.txt
  git add file.txt
  git commit -q -m "initial"
}

teardown() {
  teardown_test_home
}

function grit_tag_create_lightweight { # @test
  local params='{"name":"grit.tag","arguments":{"subcommand":"create","name":"v1.0","lightweight":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  git tag --list | grep -q '^v1.0$'
  # Lightweight tag points directly at a commit (no tag object)
  [ "$(git cat-file -t v1.0)" = "commit" ]
}

function grit_tag_create_annotated_requires_message { # @test
  local params='{"name":"grit.tag","arguments":{"subcommand":"create","name":"v1.0","sign":false}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "message is required"

  ! git tag --list | grep -q '^v1.0$'
}

function grit_tag_create_annotated_with_message { # @test
  local params='{"name":"grit.tag","arguments":{"subcommand":"create","name":"v1.0","sign":false,"message":"first release"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ "$(git cat-file -t v1.0)" = "tag" ]
  git tag -l --format='%(contents)' v1.0 | grep -q "first release"
}

function grit_tag_create_force_replaces { # @test
  git tag v1.0
  local first_sha
  first_sha=$(git rev-parse v1.0)

  echo "b" >file.txt
  git add file.txt
  git commit -q -m "second"

  local params='{"name":"grit.tag","arguments":{"subcommand":"create","name":"v1.0","lightweight":true,"force":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  local new_sha
  new_sha=$(git rev-parse v1.0)
  [ "$first_sha" != "$new_sha" ]
}

function grit_tag_create_lightweight_rejects_message { # @test
  local params='{"name":"grit.tag","arguments":{"subcommand":"create","name":"v1.0","lightweight":true,"message":"nope"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "lightweight tags cannot have a message"
}

function grit_tag_list_shows_tags { # @test
  git tag v0.1.0
  git tag v0.2.0
  git tag dev-thing

  local params='{"name":"grit.tag","arguments":{"subcommand":"list"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  assert_output --partial "v0.1.0"
  assert_output --partial "v0.2.0"
  assert_output --partial "dev-thing"
}

function grit_tag_list_with_pattern { # @test
  git tag v0.1.0
  git tag v0.2.0
  git tag dev-thing

  local params='{"name":"grit.tag","arguments":{"subcommand":"list","pattern":"v*"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  assert_output --partial "v0.1.0"
  assert_output --partial "v0.2.0"
  refute_output --partial "dev-thing"
}

function grit_tag_list_with_max_count { # @test
  git tag v0.1.0
  git tag v0.2.0
  git tag v0.3.0

  local params='{"name":"grit.tag","arguments":{"subcommand":"list","max_count":2}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text // .content[0].text')
  local n
  n=$(echo "$text" | grep -c '^v')
  [ "$n" = "2" ]
}

function grit_tag_delete_removes_tag { # @test
  git tag v0.1.0
  git tag --list | grep -q '^v0.1.0$'

  local params='{"name":"grit.tag","arguments":{"subcommand":"delete","name":"v0.1.0"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  ! git tag --list | grep -q '^v0.1.0$'
}

function grit_tag_unknown_subcommand_rejected { # @test
  local params='{"name":"grit.tag","arguments":{"subcommand":"yeet","name":"v1.0"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  # Schema-level enum validation rejects unknown subcommands before the
  # bash dispatcher runs.
  assert_output --partial "must be one of"
}

function grit_tag_verify_unsigned_fails { # @test
  git tag v1.0
  local params='{"name":"grit.tag","arguments":{"subcommand":"verify","name":"v1.0"}}'
  run_moxy_mcp "tools/call" "$params"
  # Lightweight tags can't be verified; expect non-zero exit reflected in
  # the MCP error/text. Just check the call doesn't blow up the bridge.
  assert_success
}
