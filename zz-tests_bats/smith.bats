#!/usr/bin/env bats

# bats file_tags=smith

# Tests for the smith moxin's fj invocation shapes (#392). smith wraps the
# Forgejo CLI (fj); these tests shadow fj with a stub that records argv, and
# assert the wrapper scripts build the right command lines:
#   - qualified OWNER/NAME#N refs when a repo arg is given, bare N otherwise
#   - the -H host flag threaded before the subcommand
#   - issue-create always passing --no-template and --body so fj never opens
#     an editor or an interactive template picker (moxy tools run headless)
#   - clap optional-value flags passed in = form (--with-msg=, --message=)
#     so the ref positional isn't swallowed as the flag's value
#   - fj is invoked from a scratch cwd (outside any git repo) when repo AND
#     host are both given explicitly, working around a forgejo-cli crash on
#     owner-less git remotes (#398); the real cwd is preserved when either
#     is omitted, since that means the caller relies on cwd auto-resolution

load 'common'

BIN="${SMITH_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/smith/bin}"

setup() {
  setup_test_home

  mkdir -p "$HOME/bin"
  # fj stub: append each invocation's argv (one arg per line) plus a `---`
  # separator to $HOME/fj-args, and its cwd to $HOME/fj-pwd (one line per
  # invocation), so tests can assert #398's cwd-redirect fix without a real
  # fj binary. Note: no shebang — the nix sandbox lacks /usr/bin/env.
  cat >"$HOME/bin/fj" <<'EOF'
printf '%s\n' "$@" >> "$HOME/fj-args"
printf -- '---\n' >> "$HOME/fj-args"
printf '%s\n' "$PWD" >> "$HOME/fj-pwd"
echo "fj-stub-ok"
EOF
  chmod +x "$HOME/bin/fj"

  # FJ_BIN lets the stub win regardless of wrapProgram's PATH mode.
  export FJ_BIN="$HOME/bin/fj"
}

teardown() {
  teardown_test_home
}

# A repo arg turns the issue number into a qualified OWNER/NAME#N ref, which
# fj resolves from any cwd (no local git remote needed).
function issue_comment_with_repo_uses_qualified_ref { # @test
  run "$BIN/issue-comment" 7 "hello there" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'issue\ncomment\nowner/repo#7\nhello there\n---'
}

# Without a repo arg the bare number is passed through — fj resolves the
# repo from the cwd's git remotes.
function issue_comment_without_repo_uses_bare_ref { # @test
  run "$BIN/issue-comment" 7 "hello"
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'issue\ncomment\n7\nhello\n---'
}

# The host arg becomes fj's global -H flag, before the subcommand.
function host_arg_threads_as_global_flag { # @test
  run "$BIN/issue-list" "" "" "" "" "" "" "codeberg.org"
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'-H\ncodeberg.org\nissue\nsearch\n---'
}

# issue-list threads every filter into the matching fj flag.
function issue_list_threads_filters { # @test
  run "$BIN/issue-list" "crash" "all" "bug,ux" "alice" "bob" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'issue\nsearch\n-s\nall\n-l\nbug,ux\n-c\nalice\n-a\nbob\n-r\nowner/repo\ncrash\n---'
}

# pr-list shares issue-list's filter threading (run_fj_search in .fj-common).
function pr_list_threads_filters { # @test
  run "$BIN/pr-list" "fix" "closed" "" "" "" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'pr\nsearch\n-s\nclosed\n-r\nowner/repo\nfix\n---'
}

# issue-create must always pass --no-template and --body (even empty):
# without them fj opens an interactive template picker or editor, which
# hangs a headless MCP tool call.
function issue_create_never_opens_editor { # @test
  run "$BIN/issue-create" "test title" "" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'issue\ncreate\n--no-template\n--body\n\n-r\nowner/repo\ntest title\n---'
}

# A repo arg that isn't OWNER/NAME is rejected before fj runs.
function malformed_repo_is_rejected { # @test
  run "$BIN/issue-comment" 7 "hi" "badrepo" ""
  assert_failure 2
  assert_output --partial "OWNER/NAME"

  [ ! -e "$HOME/fj-args" ] || fail "fj was invoked despite invalid repo"
}

# comments=true appends a second fj call for the comment thread.
function issue_get_with_comments_runs_two_views { # @test
  run "$BIN/issue-get" 5 "true" "" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'issue\nview\n5\nbody\n---\nissue\nview\n5\ncomments\n---'
}

# issue-close passes the closing comment via --with-msg= (the = form: the
# flag takes an optional value, so the space form would swallow the ref).
function issue_close_with_msg_uses_equals_form { # @test
  run "$BIN/issue-close" 3 "done in abc123" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'issue\nclose\n--with-msg=done in abc123\nowner/repo#3\n---'
}

# pr-merge threads method/delete/title/message flags; --message= uses the =
# form for the same clap optional-value reason as --with-msg=.
function pr_merge_threads_flags { # @test
  run "$BIN/pr-merge" 9 "squash" "true" "merge title" "merge body" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'pr\nmerge\n-M\nsquash\n-d\n-t\nmerge title\n--message=merge body\nowner/repo#9\n---'
}

# autofill=true swaps title/body for -A.
function pr_create_autofill_passes_A { # @test
  run "$BIN/pr-create" "" "" "" "" "true" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'pr\ncreate\n-r\nowner/repo\n-A\n---'
}

# Without autofill, a missing title is rejected before fj runs.
function pr_create_requires_title_without_autofill { # @test
  run "$BIN/pr-create" "" "" "" "" "false" "owner/repo" ""
  assert_failure 2
  assert_output --partial "title is required"
}

# An unknown pr-get view is rejected before fj runs.
function pr_get_rejects_unknown_view { # @test
  run "$BIN/pr-get" 4 "bogus" "" ""
  assert_failure 2
  assert_output --partial "view must be one of"
}

# When both repo and host are given, fj_run redirects fj's cwd to a scratch
# dir outside any git repo (#398) since cwd-based resolution is unneeded.
function redirects_cwd_when_repo_and_host_given { # @test
  local orig_pwd="$PWD"
  run "$BIN/issue-comment" 7 "hello" "owner/repo" "codeberg.org"
  assert_success

  run cat "$HOME/fj-pwd"
  assert_success
  [ "$output" != "$orig_pwd" ] || fail "expected fj to run outside the test cwd, got: $output"
}

# When host is omitted, the caller relies on cwd auto-resolution, so fj_run
# must leave the real cwd alone.
function no_redirect_when_host_omitted { # @test
  local orig_pwd="$PWD"
  run "$BIN/issue-comment" 7 "hello" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-pwd"
  assert_success
  assert_output "$orig_pwd"
}

# When repo is omitted, same as above: fj_run leaves the real cwd alone.
function no_redirect_when_repo_omitted { # @test
  local orig_pwd="$PWD"
  run "$BIN/issue-comment" 7 "hello" "" "codeberg.org"
  assert_success

  run cat "$HOME/fj-pwd"
  assert_success
  assert_output "$orig_pwd"
}

# issue-edit-labels threads add/rm into fj's -a/-r flags (#418).
function issue_edit_labels_threads_add_and_rm { # @test
  run "$BIN/issue-edit-labels" 7 "bug,ux" "wontfix" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'issue\nedit\nowner/repo#7\nlabels\n-a\nbug,ux\n-r\nwontfix\n---'
}

# Without add or rm there is nothing to do — rejected before fj runs.
function issue_edit_labels_requires_add_or_rm { # @test
  run "$BIN/issue-edit-labels" 7 "" "" "owner/repo" ""
  assert_failure 2
  assert_output --partial "add and/or rm"

  [ ! -e "$HOME/fj-args" ] || fail "fj was invoked despite no add/rm"
}

# repo-label-list threads the repo positional before `view`, and -a for archived.
function repo_label_list_threads_repo_and_archived { # @test
  run "$BIN/repo-label-list" "owner/repo" "" "true"
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'repo\nlabels\nowner/repo\nview\n-a\n---'
}

# Without a repo arg, fj resolves from cwd — no positional repo is passed.
function repo_label_list_without_repo_omits_positional { # @test
  run "$BIN/repo-label-list" "" "" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'repo\nlabels\nview\n---'
}

# repo-label-create threads name/color plus the optional flags, using the =
# form for --description so the value can't swallow a later positional.
function repo_label_create_threads_flags { # @test
  run "$BIN/repo-label-create" "area/ux" "00aabb" "owner/repo" "" "a ux label" "true" "true"
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'repo\nlabels\nowner/repo\ncreate\narea/ux\n00aabb\n--description=a ux label\n-e\n-a\n---'
}

# repo-label-delete threads the repo positional and the label id/name.
function repo_label_delete_threads_repo_and_id { # @test
  run "$BIN/repo-label-delete" "wontfix" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'repo\nlabels\nowner/repo\ndelete\nwontfix\n---'
}

# release-list threads -p/-d/-r (#419).
function release_list_threads_flags { # @test
  run "$BIN/release-list" "owner/repo" "" "true" "true"
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'release\nlist\n-p\n-d\n-r\nowner/repo\n---'
}

# release-view threads -t/-r before the trailing name positional.
function release_view_threads_by_tag_and_repo { # @test
  run "$BIN/release-view" "v1.0.0" "true" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'release\nview\n-t\n-r\nowner/repo\nv1.0.0\n---'
}

# release-create always passes --body= (even empty) so fj never opens an
# editor, mirroring issue-create's headless-safety convention; --create-tag=
# uses the = form since it's a clap optional-value flag.
function release_create_threads_flags { # @test
  run "$BIN/release-create" "v1.0.0" "" "v1.0.0" "release notes" "main" "true" "true" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'release\ncreate\n--body=release notes\n--create-tag=v1.0.0\n-B\nmain\n-d\n-p\n-r\nowner/repo\nv1.0.0\n---'
}

# release-delete threads -t/-r before the trailing name positional.
function release_delete_threads_by_tag_and_repo { # @test
  run "$BIN/release-delete" "v1.0.0" "true" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'release\ndelete\n-t\n-r\nowner/repo\nv1.0.0\n---'
}

# tag-list threads -p (page) and -r.
function tag_list_threads_flags { # @test
  run "$BIN/tag-list" "owner/repo" "" "2"
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'tag\nlist\n-p\n2\n-r\nowner/repo\n---'
}

# tag-view threads -r before the trailing name positional.
function tag_view_threads_repo { # @test
  run "$BIN/tag-view" "v1.0.0" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'tag\nview\n-r\nowner/repo\nv1.0.0\n---'
}

# tag-create always passes --body= (even empty), same headless-safety
# convention as release-create.
function tag_create_threads_flags { # @test
  run "$BIN/tag-create" "v1.0.0" "tag message" "main" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'tag\ncreate\n--body=tag message\n-B\nmain\n-r\nowner/repo\nv1.0.0\n---'
}

# tag-delete threads -r before the trailing name positional.
function tag_delete_threads_repo { # @test
  run "$BIN/tag-delete" "v1.0.0" "owner/repo" ""
  assert_success

  run cat "$HOME/fj-args"
  assert_output $'tag\ndelete\n-r\nowner/repo\nv1.0.0\n---'
}

# End-to-end through moxy: the smith.issue-list tool dispatches to the
# wrapped script, which invokes the (stubbed) fj off the inherited PATH.
function smith_issue_list_via_moxy { # @test
  local params='{"name":"smith.issue-list","arguments":{"repo":"owner/repo"}}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.isError != true' || fail "issue-list returned isError: $output"
  echo "$output" | jq -e '.content[0].text | contains("fj-stub-ok")' ||
    fail "expected fj stub output in result: $output"
}
