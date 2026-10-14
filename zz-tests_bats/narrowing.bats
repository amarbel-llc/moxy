#! /usr/bin/env bats

# bats file_tags=narrowing

# End-to-end coverage for moxyfile narrowing: `moxy narrows`,
# `moxy show-effective-moxyfile`, and the MOXY_PARENT_MOXYFILE refusal at
# startup and on reload. See moxyfile(5) NARROWING and
# docs/features/0013-moxyfile-narrowing.md. The serve-http cases live in
# narrowing_http.bats (they need loopback binding).

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
  # No ambient moxins: the tool lists below must contain only what each
  # test's moxyfile declares.
  export MOXIN_PATH="$BATS_TEST_TMPDIR/no-moxins"
  mkdir -p "$MOXIN_PATH" "$HOME/parent" "$HOME/child"
}

teardown() {
  teardown_test_home
}

write_parent() {
  cat >"$HOME/parent/moxyfile" <<EOF
builtin-native = false
disable-moxins = ["chix"]

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
}

write_narrowing_child() {
  cat >"$HOME/child/moxyfile" <<EOF
builtin-native = false
disable-moxins = ["chix", "grit"]

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
}

write_widening_child() {
  cat >"$HOME/child/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash", "--extra"]

[[servers]]
name = "extra"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
}

function narrows_passes_silently_for_a_narrowing_child { # @test
  write_parent
  write_narrowing_child
  run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
  assert_success
  assert_output ""
}

function narrows_prints_one_sorted_line_per_violation { # @test
  write_parent
  write_widening_child
  run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
  assert_failure 1
  assert_output "builtin-native: parent sets false, child does not
disable-moxins: parent disables chix, child does not
servers.extra: not in parent
servers.srv: command differs from parent"
}

function narrows_output_is_byte_stable_across_runs { # @test
  write_parent
  write_widening_child
  run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
  local first="$output"
  local i
  for i in 1 2 3 4 5; do
    run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
    assert_equal "$output" "$first"
  done
}

function narrows_json_reports_key_subject_and_message { # @test
  write_parent
  write_widening_child
  run_moxy narrows --json --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
  assert_failure 1
  echo "$output" | jq -e '.narrows == false' || fail "narrows flag: $output"
  echo "$output" | jq -e '.violations | length == 4' || fail "violation count: $output"
  echo "$output" | jq -e '.violations[2] == {"key":"servers.extra","subject":"extra","message":"not in parent"}' ||
    fail "violation shape: $output"

  write_narrowing_child
  run_moxy narrows --json --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
  assert_success
  assert_output '{"narrows":true,"violations":[]}'
}

function narrows_exits_2_when_a_side_cannot_be_read { # @test
  write_parent
  run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/absent"
  assert_failure 2
  assert_output --partial "--child"

  run_moxy narrows --parent "$HOME/parent/absent" --child "$HOME/parent/moxyfile"
  assert_failure 2
  assert_output --partial "--parent"

  echo 'this is = = not toml' >"$HOME/child/moxyfile"
  run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
  assert_failure 2
}

function narrows_walks_a_directory_reference_as_if_it_were_cwd { # @test
  # The global moxyfile is part of a directory's effective config but not of
  # a bare file's, so the same child passes against the directory and fails
  # against the file.
  mkdir -p "$HOME/.config/moxy"
  cat >"$HOME/.config/moxy/moxyfile" <<EOF
[[servers]]
name = "global"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
  write_parent
  cat >"$HOME/child/child.toml" <<EOF
builtin-native = false
disable-moxins = ["chix"]

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]

[[servers]]
name = "global"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  run_moxy narrows --parent "$HOME/parent" --child "$HOME/child/child.toml"
  assert_success

  run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/child.toml"
  assert_failure 1
  assert_output "servers.global: not in parent"
}

function show_effective_moxyfile_is_deterministic_and_never_expands { # @test
  mkdir -p "$HOME/.config/moxy"
  cat >"$HOME/.config/moxy/moxyfile" <<'EOF'
disable-moxins = ["zeta", "alpha"]

[[servers]]
name = "web"
url = "https://example.invalid/mcp"

[servers.headers]
Authorization = "Bearer $NARROWING_TEST_SECRET"
EOF
  cat >"$HOME/parent/extra.toml" <<'EOF'
[[servers]]
name = "included"
command = ["included-server", "an arg with spaces"]
EOF
  cat >"$HOME/parent/moxyfile" <<'EOF'
include = ["extra.toml"]
disable-moxins = ["alpha", "mid"]
EOF

  export NARROWING_TEST_SECRET=hunter2
  run_moxy show-effective-moxyfile --dir "$HOME/parent"
  assert_success
  local first="$output"
  assert_output --partial 'disable-moxins = ["alpha", "mid", "zeta"]'
  assert_output --partial 'command = ["included-server", "an arg with spaces"]'
  assert_output --partial '"Authorization" = "Bearer $NARROWING_TEST_SECRET"'
  refute_output --partial "hunter2"
  refute_output --partial "include ="

  run_moxy show-effective-moxyfile --dir "$HOME/parent"
  assert_equal "$output" "$first"

  # Without --dir it renders the working directory.
  cd "$HOME/parent"
  run_moxy show-effective-moxyfile
  assert_equal "$output" "$first"

  # The render is itself a moxyfile: it is its own fixed point, and the
  # directory it came from narrows it.
  printf '%s\n' "$first" >"$HOME/rendered.toml"
  run_moxy show-effective-moxyfile --dir "$HOME/rendered.toml"
  assert_equal "$output" "$first"
  run_moxy narrows --parent "$HOME/rendered.toml" --child "$HOME/parent"
  assert_success
  assert_output ""
}

function serve_starts_when_its_moxyfile_narrows_the_parent { # @test
  write_parent
  write_narrowing_child
  export MOXY_PARENT_MOXYFILE="$HOME/parent/moxyfile"
  cd "$HOME/child"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command")' ||
    fail "srv tool missing: $output"
}

function serve_refuses_to_start_when_its_moxyfile_widens_the_parent { # @test
  write_parent
  write_widening_child
  export MOXY_PARENT_MOXYFILE="$HOME/parent/moxyfile"
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --partial "does not narrow MOXY_PARENT_MOXYFILE"
  assert_output --partial "servers.extra: not in parent"
  assert_output --partial "servers.srv: command differs from parent"
  # Refused before anything was spawned.
  refute_output --partial "connected to"
}

function serve_fails_closed_when_the_parent_reference_is_unreadable { # @test
  write_narrowing_child
  export MOXY_PARENT_MOXYFILE="$HOME/parent/absent"
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --partial "MOXY_PARENT_MOXYFILE"
  refute_output --partial "connected to"
}

function serve_without_a_parent_reference_checks_nothing { # @test
  write_widening_child
  cd "$HOME/child"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "extra.execute-command")' ||
    fail "extra tool missing: $output"
}

# A one-tool moxin named $2 under moxin directory $1, with perms-request $3.
write_moxin() {
  mkdir -p "$1/$2"
  cat >"$1/$2/_moxin.toml" <<EOF
schema = 1
name = "$2"
EOF
  cat >"$1/$2/hello.toml" <<EOF
schema = 1
description = "Say hello"
perms-request = "$3"
command = "echo"
args = ["-n", "hello"]
EOF
}

# The parent's world for the moxin-inventory cases: one moxin directory with
# one moxin, system moxins off, and the effective moxyfile rendered from it —
# record of resolved moxins included — as the parent reference.
render_parent_with_moxins() {
  PARENT_MOXINS="$BATS_TEST_TMPDIR/parent-moxins"
  OTHER_MOXINS="$BATS_TEST_TMPDIR/other-moxins"
  write_moxin "$PARENT_MOXINS" greeter each-use
  echo 'builtin-native = false' >"$HOME/parent/moxyfile"
  echo 'builtin-native = false' >"$HOME/child/moxyfile"

  export MOXIN_PATH="$PARENT_MOXINS"
  run_moxy show-effective-moxyfile --dir "$HOME/parent"
  assert_success
  printf '%s\n' "$output" >"$HOME/parent-reference.toml"
  export MOXY_PARENT_MOXYFILE="$HOME/parent-reference.toml"
}

function show_effective_moxyfile_records_where_each_moxin_resolves { # @test
  render_parent_with_moxins
  run cat "$HOME/parent-reference.toml"
  assert_output --partial "[resolved-moxins]"
  assert_output --regexp '"greeter" = ".*/parent-moxins/greeter"'

  # A rendered file keeps the record it carries, whatever MOXIN_PATH is now.
  export MOXIN_PATH="$BATS_TEST_TMPDIR/no-moxins"
  run_moxy show-effective-moxyfile --dir "$HOME/parent-reference.toml"
  assert_success
  assert_equal "$output" "$(cat "$HOME/parent-reference.toml")"
}

function serve_starts_when_its_moxins_are_the_parents { # @test
  render_parent_with_moxins
  cd "$HOME/child"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.hello")' ||
    fail "greeter tool missing: $output"
}

function serve_refuses_a_moxin_path_that_surfaces_a_moxin_the_parent_never_had { # @test
  render_parent_with_moxins
  write_moxin "$OTHER_MOXINS" sneaky always-allow
  export MOXIN_PATH="$PARENT_MOXINS:$OTHER_MOXINS"
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --partial "does not narrow MOXY_PARENT_MOXYFILE"
  assert_output --regexp 'resolved-moxins\.sneaky: moxin from .*/other-moxins/sneaky, not in parent'
}

function serve_refuses_a_moxin_path_that_shadows_a_moxin_with_looser_tiers { # @test
  # The widening a moxyfile cannot express: same moxin name, same tool name,
  # but from a directory whose tool asks for a looser permission tier.
  render_parent_with_moxins
  write_moxin "$OTHER_MOXINS" greeter always-allow
  export MOXIN_PATH="$OTHER_MOXINS:$PARENT_MOXINS"
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --regexp "resolved-moxins\.greeter: moxin from .*/other-moxins/greeter, parent's is from .*/parent-moxins/greeter"
}

function serve_accepts_a_foreign_moxin_the_child_disables { # @test
  render_parent_with_moxins
  write_moxin "$OTHER_MOXINS" sneaky always-allow
  export MOXIN_PATH="$PARENT_MOXINS:$OTHER_MOXINS"
  cat >"$HOME/child/moxyfile" <<'EOF'
builtin-native = false
disable-moxins = ["sneaky"]
EOF
  cd "$HOME/child"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '[.tools[] | select(.name | startswith("sneaky."))] | length == 0' ||
    fail "disabled moxin is listed: $output"
}

function serve_refuses_system_moxins_the_parent_had_turned_off { # @test
  # builtin-native is judged on its own key, and the inventory agrees: with
  # the system moxins back on, the child resolves moxins the parent never did.
  render_parent_with_moxins
  : >"$HOME/child/moxyfile"
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --partial "builtin-native: parent sets false, child does not"
  assert_output --partial "resolved-moxins."
}

# Start a narrowing moxy, wait for it to finish booting (the initialize
# response), widen its moxyfile on disk, then call `restart` and `tools/list`.
# Prints the restart result and the tools/list result, one JSON object per
# line; moxy's stderr lands in $1.
restart_after_widening() {
  local stderr_file="$1"
  _ensure_madder_default_store
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local restart='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"restart","arguments":{}}}'
  local list='{"jsonrpc":"2.0","id":3,"method":"tools/list"}'
  local gate
  gate=$(mktemp -u)
  mkfifo "$gate"

  run timeout --preserve-status "20s" bash -c '
    cd "$HOME/child"
    gate="$5"
    {
      echo "$1"
      echo "$2"
      read -r <"$gate"
      cp "$HOME/widened.toml" "$HOME/child/moxyfile"
      if [[ -n ${WIDEN_PARENT_REFERENCE_TOO:-} ]]; then
        cp "$HOME/widened.toml" "$MOXY_PARENT_MOXYFILE"
      fi
      echo "$3"
      read -r <"$gate"
      echo "$4"
      sleep 2
    } | "${MOXY_BIN:-moxy}" serve mcp 2>"$6" | while IFS= read -r line; do
      id=$(echo "$line" | jq -r ".id // empty")
      case "$id" in
        1) echo ready >"$gate" ;;
        2)
          echo "$line" | jq -c ".result"
          echo ready >"$gate"
          ;;
        3) echo "$line" | jq -c ".result" ;;
      esac
    done
  ' -- "$init" "$initialized" "$restart" "$list" "$gate" "$stderr_file"

  rm -f "$gate"
}

function restart_refuses_a_widened_moxyfile_and_keeps_the_running_state { # @test
  write_parent
  write_widening_child
  mv "$HOME/child/moxyfile" "$HOME/widened.toml"
  write_narrowing_child
  export MOXY_PARENT_MOXYFILE="$HOME/parent/moxyfile"

  local stderr_file="$BATS_TEST_TMPDIR/moxy-stderr"
  restart_after_widening "$stderr_file"
  assert_success

  local restart_result list_result
  restart_result=$(sed -n 1p <<<"$output")
  list_result=$(sed -n 2p <<<"$output")

  # The refusal is the restart tool's result, violations included...
  echo "$restart_result" | jq -e '.isError == true' || fail "restart did not fail: $restart_result"
  echo "$restart_result" | jq -e '.content[0].text | test("servers.extra: not in parent")' ||
    fail "restart result lacks the violation: $restart_result"

  # ...and a line in moxy's own log.
  run cat "$stderr_file"
  assert_output --partial "reload refused, keeping the running configuration"
  assert_output --partial "servers.extra: not in parent"

  # The old state is intact: the tool that was there still is, the widened
  # one never appeared.
  echo "$list_result" | jq -e '.tools[] | select(.name == "srv.execute-command")' ||
    fail "srv tool lost after refused reload: $list_result"
  echo "$list_result" | jq -e '[.tools[] | select(.name | startswith("extra."))] | length == 0' ||
    fail "widened tool appeared after refused reload: $list_result"
}

function restart_judges_against_the_parent_reference_read_at_startup { # @test
  # Whoever can write the reference file must not be able to widen a running
  # moxy by rewriting it: the reference is read once, at startup.
  write_parent
  write_widening_child
  mv "$HOME/child/moxyfile" "$HOME/widened.toml"
  write_narrowing_child
  cp "$HOME/parent/moxyfile" "$HOME/reference.toml"
  export MOXY_PARENT_MOXYFILE="$HOME/reference.toml"
  export WIDEN_PARENT_REFERENCE_TOO=1

  restart_after_widening "$BATS_TEST_TMPDIR/moxy-stderr"
  assert_success

  local restart_result list_result
  restart_result=$(sed -n 1p <<<"$output")
  list_result=$(sed -n 2p <<<"$output")
  echo "$restart_result" | jq -e '.isError == true and (.content[0].text | test("servers.extra: not in parent"))' ||
    fail "rewriting the reference let the reload through: $restart_result"
  echo "$list_result" | jq -e '[.tools[] | select(.name | startswith("extra."))] | length == 0' ||
    fail "widened tool appeared: $list_result"
}

function restart_refuses_promoting_a_pass_held_tool_to_use { # @test
  # moxy#461: srv is held at pass by both the parent reference and this
  # session's own startup moxyfile — conferrable to a child, never callable
  # by this session itself. Moving it into allow-tools and calling `restart`
  # must not let the session promote itself to use what it only held at
  # pass; that would otherwise pass the ordinary allow-tools/pass-tools
  # relation (a pass holder may let a *child* use it) even though nothing
  # here is spawning a child.
  cat >"$HOME/parent/moxyfile" <<EOF
builtin-native = false
disable-moxins = ["chix"]
allow-tools = ["restart"]
pass-tools = ["srv"]

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
  cp "$HOME/parent/moxyfile" "$HOME/child/moxyfile"
  cat >"$HOME/widened.toml" <<EOF
builtin-native = false
disable-moxins = ["chix"]
allow-tools = ["restart", "srv"]

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
  export MOXY_PARENT_MOXYFILE="$HOME/parent/moxyfile"

  local stderr_file="$BATS_TEST_TMPDIR/moxy-stderr"
  restart_after_widening "$stderr_file"
  assert_success

  local restart_result list_result
  restart_result=$(sed -n 1p <<<"$output")
  list_result=$(sed -n 2p <<<"$output")

  echo "$restart_result" | jq -e '.isError == true' || fail "restart did not fail: $restart_result"
  echo "$restart_result" | jq -e '.content[0].text | test("allow-tools: held by the parent at pass")' ||
    fail "restart result lacks the pass-not-promoted violation: $restart_result"
  echo "$list_result" | jq -e '[.tools[] | select(.name | startswith("srv."))] | length == 0' ||
    fail "pass-held tool became callable after the reload: $list_result"
}

function restart_still_allows_an_ordinary_reload_with_no_pass_tools_involved { # @test
  # Confirms the moxy#461 fix is additive: a reload that touches nothing
  # held at pass still narrows and succeeds exactly as before.
  write_parent
  write_narrowing_child
  cp "$HOME/child/moxyfile" "$HOME/widened.toml"
  export MOXY_PARENT_MOXYFILE="$HOME/parent/moxyfile"

  restart_after_widening "$BATS_TEST_TMPDIR/moxy-stderr"
  assert_success

  local restart_result
  restart_result=$(sed -n 1p <<<"$output")
  echo "$restart_result" | jq -e '.isError != true' || fail "unrelated reload was refused: $restart_result"
}

function serve_refuses_an_empty_parent_reference { # @test
  # An empty file would parse as a parent that constrains almost nothing; it
  # is far likelier a render that failed.
  write_narrowing_child
  : >"$HOME/reference.toml"
  export MOXY_PARENT_MOXYFILE="$HOME/reference.toml"
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --partial "MOXY_PARENT_MOXYFILE"
  assert_output --partial "is empty"
  refute_output --partial "connected to"
}

function serve_refuses_a_relative_parent_reference_path { # @test
  write_parent
  write_narrowing_child
  cp "$HOME/parent/moxyfile" "$HOME/child/reference.toml"
  export MOXY_PARENT_MOXYFILE="reference.toml"
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --partial "is not an absolute path"
}

function serve_treats_a_reference_without_a_moxin_record_as_having_no_moxins { # @test
  # A hand-written reference says nothing about moxins. For a serving moxy
  # that must not mean "any": every moxin it can reach is then refused.
  write_parent
  write_narrowing_child
  write_moxin "$BATS_TEST_TMPDIR/some-moxins" greeter each-use
  export MOXIN_PATH="$BATS_TEST_TMPDIR/some-moxins"
  export MOXY_PARENT_MOXYFILE="$HOME/parent/moxyfile"
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --regexp 'resolved-moxins\.greeter: moxin from .*/some-moxins/greeter, not in parent'
}

function a_mistyped_restriction_is_an_error_not_no_restriction { # @test
  # `allow-tools` as a string must not decode to "unset, everything allowed".
  cat >"$HOME/child/moxyfile" <<'EOF'
allow-tools = "srv.execute-command"
EOF
  cd "$HOME/child"
  run_moxy serve mcp
  assert_failure
  assert_output --partial "allow-tools: wrong type"

  write_parent
  run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
  assert_failure 2
  assert_output --partial "allow-tools: wrong type"
}

function narrows_exits_2_not_1_when_its_arguments_are_malformed { # @test
  # Exit 1 means "the child widens". A flag with no value never got that far.
  write_parent
  run_moxy narrows --child "$HOME/parent/moxyfile" --parent
  assert_failure 2

  run_moxy narrows --parent "$HOME/parent/moxyfile"
  assert_failure 2
}

function narrows_judges_a_directory_childs_moxins_as_serving_it_would { # @test
  render_parent_with_moxins
  unset MOXY_PARENT_MOXYFILE

  run_moxy narrows --parent "$HOME/parent-reference.toml" --child "$HOME/child"
  assert_success
  assert_output ""

  write_moxin "$OTHER_MOXINS" sneaky always-allow
  export MOXIN_PATH="$PARENT_MOXINS:$OTHER_MOXINS"
  run_moxy narrows --parent "$HOME/parent-reference.toml" --child "$HOME/child"
  assert_failure 1
  assert_output --regexp '^resolved-moxins\.sneaky: moxin from .*/other-moxins/sneaky, not in parent$'
}
