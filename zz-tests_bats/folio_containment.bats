#! /usr/bin/env bats

# bats file_tags=folio

# moxy#532: what a folio read-side tool reaches matches what its permission
# check judged, which is the `path` input. Nothing else a call passes may take
# it outside that directory. $HOME/elsewhere stands for everything outside.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  PROJECT="$HOME/project"
  OUTSIDE="$HOME/elsewhere"
  mkdir -p "$PROJECT/sub" "$OUTSIDE/dir"
  echo in >"$PROJECT/sub/inside.txt"
  echo out >"$OUTSIDE/outside-marker.txt"
  echo out >"$OUTSIDE/dir/outside-marker-2.txt"
}

teardown() {
  teardown_test_home
}

# call TOOL ARGS_JSON: runs the tool from $PROJECT, sets $output.
call() {
  cd "$PROJECT"
  run_moxy_mcp_v1 "tools/call" "$(jq -cn --arg n "$1" --argjson a "$2" '{name: $n, arguments: $a}')"
  assert_success
}

text_of() { jq -r '.content[0].text' <<<"$output"; }

assert_ok() { jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"; }

assert_error() { jq -e '.isError == true' <<<"$output" >/dev/null || fail "expected an error: $output"; }

# No outside name appears in the result.
assert_nothing_outside() {
  [[ $output != *outside-marker* ]] || fail "reached outside: $output"
}

# assert_flag_refused TOOL: the whole result is the flag check's one-line
# refusal, so the tool never ran. (It quotes the refused word, which in some
# cases here is the caller's own spelling of an outside name.)
assert_flag_refused() {
  assert_error
  local text
  text=$(text_of)
  [[ $text == "ERROR: $1 flag not allowed: "* && $(grep -c . <<<"$text") == 1 ]] ||
    fail "not refused by the flag check: $output"
}

glob() { call folio.glob "$(jq -cn --arg p "$1" '{pattern: $p}')"; }

function glob_refuses_an_absolute_pattern { # @test
  glob "$OUTSIDE/*"
  assert_error
  assert_nothing_outside
  [[ $(text_of) == *"must stay under path"* ]] || fail "$output"
}

function glob_refuses_a_parent_segment_in_any_brace_alternative { # @test
  local p
  for p in '../elsewhere/*' 'sub/../../elsewhere/*' '{sub,../elsewhere}/*' '.{.,x}/elsewhere/*' '..'; do
    glob "$p"
    assert_error
    assert_nothing_outside
  done
}

function glob_never_returns_a_match_outside_path { # @test
  ln -s "$OUTSIDE" "$PROJECT/way-out"
  # an escaped or bracketed dot spells a parent segment without writing one,
  # and a linked directory leads out without any
  local p
  for p in '\.\./elsewhere/*' '[.][.]/elsewhere/*' '.*/elsewhere/*' 'way-out/*' '*/outside-marker*' 'way-out/*/'; do
    glob "$p"
    assert_nothing_outside
    [[ $(text_of) != *"directories matched"* ]] || fail "counted outside directories for $p: $output"
  done
}

function glob_still_matches_inside_through_an_inside_link { # @test
  ln -s sub "$PROJECT/sub-link"
  glob 'sub-link/*'
  assert_ok
  [[ $(text_of) == sub-link/inside.txt ]] || fail "$output"
}

function glob_refuses_a_path_linking_out_of_the_policy { # @test
  ln -s "$OUTSIDE" "$PROJECT/way-out"
  call folio.glob '{"pattern": "*", "path": "way-out"}'
  assert_error
  assert_nothing_outside
}

function glob_takes_an_outside_directory_as_path { # @test
  # `path` is the input the permission check judges, so this stays possible.
  call folio.glob "$(jq -cn --arg d "$OUTSIDE" '{pattern: "*.txt", path: $d}')"
  assert_ok
  [[ $(text_of) == outside-marker.txt ]] || fail "$output"
}

function ls_flags_take_no_operand { # @test
  local f
  for f in "-la $OUTSIDE" "-d $OUTSIDE/outside-marker.txt" '-l ../elsewhere' '-- ../elsewhere' '-I x ../elsewhere'; do
    call folio.ls "$(jq -cn --arg f "$f" '{path: "sub", flags: $f}')"
    assert_flag_refused ls
  done
}

function ls_flags_are_not_expanded_as_globs { # @test
  : >"$PROJECT/sub/-L"
  call folio.ls '{"path": "sub", "flags": "-?"}'
  assert_flag_refused ls
}

function ls_refuses_flags_that_follow_links { # @test
  ln -s "$OUTSIDE" "$PROJECT/sub/way-out"
  local f
  for f in -RL -lRH '-R --dereference' --dereference-command-line --derefer; do
    call folio.ls "$(jq -cn --arg f "$f" '{path: "sub", flags: $f}')"
    assert_error
    assert_nothing_outside
  done
}

function ls_keeps_the_ordinary_flags { # @test
  local f
  for f in -la -lh -1 -R '-l -a -t -r' '--all --sort=size' '--color=never -F'; do
    call folio.ls "$(jq -cn --arg f "$f" '{path: "sub", flags: $f}')"
    assert_ok
    [[ $(text_of) == *inside.txt* ]] || fail "$f: $output"
  done
}

function tree_refuses_to_follow_links { # @test
  ln -s "$OUTSIDE" "$PROJECT/sub/way-out"
  local extra
  for extra in '{"follow_symlinks": true}' '{"follow_symlinks": true, "json": true}' '{"follow_symlinks": true, "depth": 0}'; do
    call folio.tree "$(jq -cn --argjson e "$extra" '{path: "sub"} + $e')"
    assert_error
    assert_nothing_outside
    [[ $(text_of) == *"follow_symlinks is not supported"* ]] || fail "$output"
  done
}

function tree_lists_a_link_without_entering_it { # @test
  ln -s "$OUTSIDE" "$PROJECT/sub/way-out"
  local extra
  for extra in '{}' '{"follow_symlinks": false}' '{"depth": 0}'; do
    call folio.tree "$(jq -cn --argjson e "$extra" '{path: "sub"} + $e')"
    assert_ok
    [[ $(text_of) == *way-out* && $(text_of) == *inside.txt* ]] || fail "$output"
    # the link's own target is shown, nothing under it
    [[ $output != *outside-marker* ]] || fail "reached outside: $output"
  done
}

function du_flags_take_no_operand { # @test
  local f
  for f in "-a $OUTSIDE" "$OUTSIDE" '-a ../elsewhere' '-d 1 ../elsewhere' '-- ../elsewhere'; do
    call folio.du "$(jq -cn --arg f "$f" '{path: "sub", flags: $f}')"
    assert_flag_refused du
  done
}

function du_refuses_flags_that_follow_links_or_read_files { # @test
  ln -s "$OUTSIDE" "$PROJECT/sub/way-out"
  printf '%s\0' "$OUTSIDE/outside-marker.txt" >"$PROJECT/sub/list"
  local f
  for f in -aL '-a --dereference' '--files0-from=sub/list' '--files0-from=list' "--exclude-from=$OUTSIDE/outside-marker.txt" "-X $OUTSIDE/outside-marker.txt" '--exclude-f=list'; do
    call folio.du "$(jq -cn --arg f "$f" '{path: "sub", flags: $f}')"
    assert_flag_refused du
  done
}

function du_keeps_the_ordinary_flags { # @test
  local f
  for f in -a -ah -c '-a -d 1' -ad1 '--all --max-depth=1' '-a --exclude=*.log' '-a --apparent-size -b'; do
    call folio.du "$(jq -cn --arg f "$f" '{path: "sub", flags: $f}')"
    assert_ok
    [[ $(text_of) == *sub* ]] || fail "$f: $output"
  done
}
