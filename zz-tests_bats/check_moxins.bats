#! /usr/bin/env bats

# bats file_tags=perms_matrix

# `moxy check-moxins --dir DIR` loads a moxin directory with the server's
# loader and reports every failing tool file; a repo that ships moxins runs it
# as its gate (moxy#520 follow-up).

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

# make_moxin DIR NAME: a moxin NAME under DIR with one good tool.
make_moxin() {
  local dir=$1 name=$2
  mkdir -p "$dir/$name"
  printf 'schema = 1\nname = "%s"\n' "$name" >"$dir/$name/_moxin.toml"
  cat >"$dir/$name/good.toml" <<'EOF'
schema = 3
command = "echo"
perms-request = "each-use"
EOF
}

# bad_tool DIR NAME FILE: an always-allow tool with a path input and no reason.
bad_tool() {
  cat >"$1/$2/$3.toml" <<'EOF'
schema = 3
command = "echo"
perms-request = "always-allow"

[input]
type = "object"

[input.properties.path]
type = "string"
EOF
}

function check_moxins_passes_a_directory_of_good_moxins { # @test
  make_moxin "$BATS_TEST_TMPDIR/ms" one
  make_moxin "$BATS_TEST_TMPDIR/ms" two
  run "${MOXY_BIN:-moxy}" check-moxins --dir "$BATS_TEST_TMPDIR/ms"
  [ "$status" -eq 0 ]
  [[ $output == *"ok: 2 moxins, 2 tools"* ]]
}

function check_moxins_accepts_a_single_moxin_directory { # @test
  make_moxin "$BATS_TEST_TMPDIR/ms" one
  run "${MOXY_BIN:-moxy}" check-moxins --dir "$BATS_TEST_TMPDIR/ms/one"
  [ "$status" -eq 0 ]
  [[ $output == *"ok: 1 moxins"* ]]
}

function check_moxins_lists_every_failing_tool_file_and_fails { # @test
  make_moxin "$BATS_TEST_TMPDIR/ms" broken
  bad_tool "$BATS_TEST_TMPDIR/ms" broken first
  bad_tool "$BATS_TEST_TMPDIR/ms" broken second
  make_moxin "$BATS_TEST_TMPDIR/ms" fine
  run "${MOXY_BIN:-moxy}" check-moxins --dir "$BATS_TEST_TMPDIR/ms"
  [ "$status" -eq 1 ]
  [[ $output == *"2 tool files"* ]]
  [[ $output == *"first.toml"* ]]
  [[ $output == *"second.toml"* ]]
  [[ $output == *"path-exempt"* ]]
  [[ $output == *"1 of 2 moxins failed"* ]]
}

function check_moxins_fails_when_there_is_no_moxin { # @test
  mkdir -p "$BATS_TEST_TMPDIR/empty"
  run "${MOXY_BIN:-moxy}" check-moxins --dir "$BATS_TEST_TMPDIR/empty"
  [ "$status" -ne 0 ]
  [[ $output == *"no moxin found"* ]]
}

function check_moxins_accepts_the_bundled_moxins { # @test
  # The source tree's own moxins are the standing fixture: they must load.
  [ -d "$BATS_TEST_DIRNAME/../moxins" ] || skip "no source tree moxins here"
  run "${MOXY_BIN:-moxy}" check-moxins --dir "$BATS_TEST_DIRNAME/../moxins"
  [ "$status" -eq 0 ]
}
