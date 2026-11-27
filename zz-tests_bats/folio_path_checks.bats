#! /usr/bin/env bats

# bats file_tags=folio

# moxy#533 and moxy#534: caller input is never read as a command option, and
# the path the checks judge is the one that gets opened. $HOME/elsewhere stands
# for everything outside the policy.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  PROJECT="$HOME/project"
  OUTSIDE="$HOME/elsewhere"
  mkdir -p "$PROJECT/sub" "$OUTSIDE/dir"
  echo in >"$PROJECT/sub/inside.txt"
  echo outside-secret >"$OUTSIDE/outside-marker.txt"
  cd "$PROJECT"
}

teardown() {
  teardown_test_home
}

call() {
  cd "$PROJECT"
  run_moxy_mcp_v1 "tools/call" "$(jq -cn --arg n "$1" --argjson a "$2" '{name: $n, arguments: $a}')"
  assert_success
}

text_of() { jq -r '.content[0].text' <<<"$output"; }

assert_ok() { jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"; }

assert_error() { jq -e '.isError == true' <<<"$output" >/dev/null || fail "expected an error: $output"; }

# ----- folio.tar inputs are operands (#533) -----

make_ball() {
  (cd "$PROJECT/sub" && tar -cf ../ball.tar inside.txt)
}

function tar_member_is_never_an_option { # @test
  make_ball
  printf '#!/bin/sh\ntouch "%s"\nexec cat\n' "$PROJECT/ran" >"$PROJECT/prog"
  chmod +x "$PROJECT/prog"
  call folio.tar "$(jq -cn --arg m "--use-compress-program=$PROJECT/prog" '{path: "ball.tar", member: $m}')"
  assert_error
  [ ! -e "$PROJECT/ran" ] || fail "member ran a program: $output"
}

function tar_still_extracts_a_member { # @test
  make_ball
  call folio.tar '{"path": "ball.tar", "member": "inside.txt"}'
  assert_ok
  [[ $(text_of) == in ]] || fail "$output"
}

function tar_pattern_is_never_an_option { # @test
  make_ball
  # as an option this reads the pattern list from a file outside the policy,
  # which here matches every member
  echo . >"$OUTSIDE/patterns"
  call folio.tar "$(jq -cn --arg p "-f$OUTSIDE/patterns" '{path: "ball.tar", pattern: $p}')"
  [[ $output != *inside.txt* ]] || fail "pattern was read as an option: $output"
  call folio.tar '{"path": "ball.tar", "pattern": "--help"}'
  [[ $output != *[Uu]sage:* ]] || fail "pattern was read as an option: $output"
  call folio.tar '{"path": "ball.tar", "pattern": "^insi"}'
  assert_ok
  [[ $(text_of) == inside.txt ]] || fail "$output"
}

# ----- pass-through forms are exact (#534) -----

perms() {
  PERMS="$MOXIN_PATH/folio/bin/folio-perms"
  [ -x "$PERMS" ] || skip "folio-perms not at $PERMS"
  cd "$PROJECT"
  PWD="$PROJECT" run "$PERMS" "$@"
}

function perms_passes_only_the_exact_descriptor_and_blob_forms { # @test
  perms read /dev/fd/3
  assert_success
  perms read madder://blobs/blake2b256-abc123
  assert_success
  local p
  # none of these is a substituted form, so each is the path it spells, and
  # each spells one outside the policy (/dev/fd/ with nothing after it is a
  # directory under /proc)
  for p in /dev/fd/../../../etc/passwd "/dev/fd/3/../../../..$OUTSIDE/outside-marker.txt" /dev/fd/ \
    "/dev/fd/$OUTSIDE" "madder://blobs/../../..$OUTSIDE/outside-marker.txt"; do
    perms read "$p"
    [ "$status" -ne 0 ] || fail "read allowed $p"
    perms write "$p"
    [ "$status" -ne 0 ] || fail "write allowed $p"
  done
  # and one that spells a path inside the working directory is allowed as one
  perms read 'madder://blobs/a/b'
  assert_success
}

function perms_write_never_passes_a_descriptor_or_blob_form_through { # @test
  # a write target of either shape is judged as the path it spells
  ln -s "$OUTSIDE" "$PROJECT/madder:"
  perms write madder://blobs/abc
  [ "$status" -ne 0 ] || fail "write allowed through a linked madder: directory"
  perms write /dev/fd/3
  [ "$status" -ne 0 ] || fail "write allowed to a descriptor path"
}

function perms_resolves_a_final_dot_dot_dot_or_slash { # @test
  mkdir -p "$PROJECT/deeper"
  ln -s "$OUTSIDE/dir" "$PROJECT/way-out"
  local p
  for p in .. ../ deeper/../.. sub/../.. way-out/ way-out/. way-out/..; do
    perms read "$p"
    [ "$status" -ne 0 ] || fail "read allowed $p"
    perms write "$p"
    [ "$status" -ne 0 ] || fail "write allowed $p"
  done
  for p in . ./ sub/.. sub/ sub/.; do
    perms read "$p"
    [ "$status" -eq 0 ] || fail "read refused $p: $output"
  done
}

function readers_refuse_a_descriptor_path_that_climbs_out { # @test
  # under enforce the server itself resolves each tool's permission check, as
  # the hook does in front of it otherwise: an ask is refused
  printf 'permission-mode = "enforce"\n' >"$PROJECT/moxyfile"
  call folio.read '{"file_path": "sub/inside.txt"}'
  assert_ok
  local p="/dev/fd/../../..$OUTSIDE"
  call folio.read "$(jq -cn --arg p "$p/outside-marker.txt" '{file_path: $p}')"
  assert_error
  [[ $output != *outside-secret* ]] || fail "read reached it: $output"
  local tool
  for tool in folio.ls folio.tree folio.du folio.df folio.file-type; do
    call "$tool" "$(jq -cn --arg p "$p" '{path: $p}')"
    assert_error
    [[ $(text_of) == "moxy denied"* ]] || fail "$tool was not refused: $output"
  done
  call folio.glob "$(jq -cn --arg p "$p" '{pattern: "*", path: $p}')"
  assert_error
  [[ $(text_of) == "moxy denied"* ]] || fail "glob was not refused: $output"
}

function readers_without_blob_inputs_refuse_a_blob_uri_as_a_path { # @test
  mkdir -p "$PROJECT/madder:/blobs"
  ln -s "$OUTSIDE/dir" "$PROJECT/madder:/blobs/abc"
  : >"$OUTSIDE/dir/outside-marker-2.txt"
  local tool
  for tool in folio.ls folio.tree folio.du folio.df; do
    call "$tool" '{"path": "madder://blobs/abc"}'
    assert_error
    [[ $output != *outside-marker* ]] || fail "$tool reached it: $output"
  done
  call folio.glob '{"pattern": "*", "path": "madder://blobs/abc"}'
  assert_error
  [[ $output != *outside-marker* ]] || fail "glob reached it: $output"
}

# ----- the literal value "null" is a path like any other (#534) -----

function a_path_named_null_is_guarded { # @test
  ln -s "$OUTSIDE" "$PROJECT/null"
  local tool
  for tool in folio.ls folio.tree folio.du folio.df folio.file-type; do
    call "$tool" '{"path": "null"}'
    assert_error
    [[ $output != *outside-marker* ]] || fail "$tool reached it: $output"
  done
  call folio.read '{"file_path": "null"}'
  assert_error
  call folio.glob '{"pattern": "*", "path": "null"}'
  assert_error
  [[ $output != *outside-marker* ]] || fail "glob reached it: $output"
}

function a_json_null_path_still_means_omitted { # @test
  call folio.ls '{"path": null}'
  assert_ok
  [[ $(text_of) == *sub* ]] || fail "$output"
}

# ----- df goes through readguard (#534) -----

function df_refuses_a_link_out_of_the_policy { # @test
  ln -s "$OUTSIDE" "$PROJECT/way-out"
  call folio.df '{"path": "way-out"}'
  assert_error
  call folio.df '{"path": "sub"}'
  assert_ok
  call folio.df '{"path": "sub/inside.txt"}'
  assert_ok
  call folio.df '{}'
  assert_ok
}
