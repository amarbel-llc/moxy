#! /usr/bin/env bats

# bats file_tags=read_symlinks

# moxy#490 follow-up (docs/plans/2026-11-11-490-read-symlink-leaves.md): the
# read tools open the judged path without following a symlink the read policy
# did not allow. A symlink leaf is followed only when its resolved target is
# itself inside the policy; otherwise the call is refused naming the path.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  PROJECT="$HOME/project"
  mkdir -p "$PROJECT" "$HOME/elsewhere"
  printf 'inside-content\n' >"$PROJECT/real.txt"
  echo outside-secret >"$HOME/elsewhere/b.txt"
  ln -s real.txt "$PROJECT/in-link"
  ln -s "$HOME/elsewhere/b.txt" "$PROJECT/out-link"
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

assert_ok() { jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"; }

assert_error() { jq -e '.isError == true' <<<"$output" >/dev/null || fail "expected an error: $output"; }

function folio_read_follows_a_link_to_a_file_inside_the_policy { # @test
  call folio.read '{"file_path": "in-link"}'
  assert_ok
  [[ $output == *inside-content* ]] || fail "$output"
}

function folio_read_refuses_a_link_out_of_the_policy_naming_the_path { # @test
  call folio.read '{"file_path": "out-link"}'
  assert_error
  [[ $output != *outside-secret* ]] || fail "leaked: $output"
  [[ $output == *out-link* && $output == *"outside the read policy"* ]] || fail "unclear error: $output"
}

function jq_follows_an_inside_link_and_refuses_an_outside_one { # @test
  echo '{"a": 1}' >"$PROJECT/data.json"
  ln -s data.json "$PROJECT/data-link"
  ln -s "$HOME/elsewhere/b.txt" "$PROJECT/data-out"
  call jq.jq '{"filter": ".a", "files": ["data-link"]}'
  assert_ok
  [[ $output == *1* ]] || fail "$output"
  call jq.jq '{"filter": ".", "files": ["data-out"], "flags": ["-R"]}'
  assert_error
  [[ $output != *outside-secret* ]] || fail "leaked: $output"
}

function rg_follows_an_inside_link_and_refuses_an_outside_one { # @test
  call rg.search '{"pattern": "inside", "path": "in-link", "output_mode": "content"}'
  assert_ok
  [[ $output == *inside-content* ]] || fail "$output"
  call rg.search '{"pattern": "secret", "path": "out-link", "output_mode": "content"}'
  assert_error
  [[ $output != *outside-secret* ]] || fail "leaked: $output"
}

function rg_directory_output_keeps_the_callers_name { # @test
  mkdir -p "$PROJECT/sub" "$HOME/elsewhere/dir"
  echo needle >"$PROJECT/sub/n.txt"
  echo needle >"$HOME/elsewhere/dir/n.txt"
  ln -s sub "$PROJECT/sub-link"
  ln -s "$HOME/elsewhere/dir" "$PROJECT/dir-out"
  call rg.search '{"pattern": "needle", "path": "sub"}'
  assert_ok
  [[ $(jq -r '.content[0].text' <<<"$output") == sub/n.txt ]] || fail "$output"
  call rg.search '{"pattern": "needle", "path": "sub-link", "output_mode": "content"}'
  assert_ok
  [[ $(jq -r '.content[0].text' <<<"$output") == "sub-link/n.txt:1:needle" ]] || fail "$output"
  call rg.search '{"pattern": "needle", "path": "dir-out"}'
  assert_error
}

function jq_cwd_is_entered_by_descriptor_and_an_outside_cwd_refused { # @test
  mkdir -p "$PROJECT/sub" "$HOME/elsewhere/dir"
  echo '{"v": 7}' >"$PROJECT/sub/d.json"
  ln -s "$HOME/elsewhere/dir" "$PROJECT/dir-out"
  call jq.jq '{"filter": ".v", "files": ["d.json"], "cwd": "sub"}'
  assert_ok
  [[ $output == *7* ]] || fail "$output"
  call jq.jq '{"filter": ".", "files": ["x.json"], "cwd": "dir-out"}'
  assert_error
}

function a_directory_swapped_for_a_link_out_is_refused_at_open { # @test
  floor="$BATS_TEST_TMPDIR/floor"
  printf '#!/bin/sh\ncase "$2" in */elsewhere*) exit 1;; esac\nexit 0\n' >"$floor"
  chmod +x "$floor"
  guard="${READGUARD_BIN:-readguard}"
  mkdir -p "$HOME/elsewhere/dir" "$PROJECT/d"
  touch "$PROJECT/d/inside" "$HOME/elsewhere/dir/outside"
  cd "$PROJECT"
  run "$guard" exec --floor "$floor" --any-args 1 -- ls d
  assert_success
  [[ $output == inside ]] || fail "$output"
  rm -r d
  ln -s "$HOME/elsewhere/dir" d
  run "$guard" exec --floor "$floor" --any-args 1 -- ls d
  assert_failure
  [[ $output != *dir/outside* ]] || fail "leaked: $output"
}

text_of() { jq -r '.content[0].text' <<<"$output"; }

# The directory readers enter the directory by descriptor and report it under
# the name the caller gave.
function directory_readers_follow_an_inside_link_and_keep_the_callers_name { # @test
  mkdir -p "$PROJECT/sub"
  echo hello >"$PROJECT/sub/f.txt"
  ln -s sub "$PROJECT/sub-link"
  call folio.ls '{"path": "sub-link"}'
  assert_ok
  [[ $(text_of | jq -r '.[0].name') == f.txt ]] || fail "$output"
  call folio.ls '{"path": "sub-link", "flags": "-R"}'
  assert_ok
  [[ $(text_of | head -n1) == "sub-link:" ]] || fail "$output"
  call folio.tree '{"path": "sub-link"}'
  assert_ok
  [[ $(text_of | head -n1) == sub-link ]] || fail "$output"
  call folio.tree '{"path": "sub-link", "json": true}'
  assert_ok
  [[ $(text_of | jq -r '.[0].name') == sub-link ]] || fail "$output"
  call folio.du '{"path": "sub-link"}'
  assert_ok
  [[ $(text_of | jq -r .path) == sub-link ]] || fail "$output"
  call folio.du '{"path": "sub-link", "flags": "-a"}'
  assert_ok
  [[ $(text_of) == *$'\tsub-link/f.txt'* && $(text_of) == *$'\tsub-link' ]] || fail "$output"
  call folio.file-type '{"path": "sub-link"}'
  assert_ok
  [[ $(text_of) == "sub-link: directory" ]] || fail "$output"
}

function directory_readers_refuse_a_link_out_of_the_policy { # @test
  mkdir -p "$HOME/elsewhere/dir"
  echo outside-secret >"$HOME/elsewhere/dir/s.txt"
  ln -s "$HOME/elsewhere/dir" "$PROJECT/dir-out"
  for call_args in 'folio.ls {"path":"dir-out"}' 'folio.tree {"path":"dir-out"}' 'folio.du {"path":"dir-out"}' 'folio.file-type {"path":"dir-out"}'; do
    call "${call_args%% *}" "${call_args#* }"
    assert_error
    [[ $output == *dir-out* && $output != *s.txt* ]] || fail "$call_args: $output"
  done
}

function file_readers_follow_an_inside_link_and_refuse_an_outside_one { # @test
  ln -s real.txt "$PROJECT/file-link"
  call folio.file-type '{"path": "file-link"}'
  assert_ok
  [[ $(text_of) == "file-link: ASCII text" ]] || fail "$output"
  call folio.du '{"path": "file-link"}'
  assert_ok
  [[ $(text_of | jq -r .bytes) == 15 ]] || fail "$output"
  call chix.nix-hash '{"path": "file-link"}'
  assert_ok
  call chix.nix-hash '{"path": "out-link"}'
  assert_error
  [[ $output != *outside-secret* ]] || fail "leaked: $output"
  call folio.file-type '{"path": "out-link"}'
  assert_error
  call folio.du '{"path": "out-link"}'
  assert_error
}

function tar_follows_an_inside_link_and_refuses_an_outside_one { # @test
  (cd "$PROJECT" && tar -cf ball.tar real.txt)
  cp "$PROJECT/ball.tar" "$HOME/elsewhere/ball.tar"
  ln -s ball.tar "$PROJECT/ball-link"
  ln -s "$HOME/elsewhere/ball.tar" "$PROJECT/ball-out"
  call folio.tar '{"path": "ball-link"}'
  assert_ok
  [[ $(text_of) == real.txt ]] || fail "$output"
  call folio.tar '{"path": "ball-out"}'
  assert_error
}

function arboretum_outline_refuses_a_link_out_of_the_policy { # @test
  printf 'package x\n\nfunc F() {}\n' >"$HOME/elsewhere/x.go"
  ln -s "$HOME/elsewhere/x.go" "$PROJECT/x.go"
  call arboretum.outline "$(jq -cn --arg p "$PROJECT/x.go" '{path: $p}')"
  assert_error
  [[ $output == *x.go* ]] || fail "$output"
}

# A path the gate judged fine by name that is a link out of the policy by the
# time it is opened (the swap) is refused: the guard judges the target. The
# fake floor stands for the policy: everything under elsewhere/ asks.
function a_leaf_swapped_for_a_link_out_is_refused_at_open { # @test
  floor="$BATS_TEST_TMPDIR/floor"
  printf '#!/bin/sh\ncase "$2" in */elsewhere/*) exit 1;; esac\nexit 0\n' >"$floor"
  chmod +x "$floor"
  guard="${READGUARD_BIN:-readguard}"
  cd "$PROJECT"
  printf 'plain\n' >swapped.txt
  run "$guard" exec --floor "$floor" --file-args 1 -- cat swapped.txt
  assert_success
  assert_output plain
  rm swapped.txt
  ln -s "$HOME/elsewhere/b.txt" swapped.txt
  run "$guard" exec --floor "$floor" --file-args 1 -- cat swapped.txt
  assert_failure
  [[ $output != *outside-secret* ]] || fail "leaked: $output"
  [[ $output == *swapped.txt* ]] || fail "error does not name the path: $output"
}
