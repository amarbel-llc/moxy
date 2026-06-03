#!/usr/bin/env bats

# bats file_tags=get_hubbed

# Tests for get-hubbed copy-file and copy-tree tools. Covers #272.

load 'common'

BIN="${GET_HUBBED_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/get-hubbed/bin}"

setup() {
  setup_test_home

  # Create a git repo with an origin remote so .resolve-repo works
  REPO="$HOME/work"
  git init -q -b main "$REPO"
  cd "$REPO"
  git config user.email t@t
  git config user.name t
  git config commit.gpgSign false
  git remote add origin "git@github.com:test-org/test-repo.git"
  git commit --allow-empty -m base -q

  # gh stub: handles the API calls used by copy-file and copy-tree.
  # Uses jq to process --jq flags, mirrors real gh behaviour.
  mkdir -p "$HOME/bin"
  # Note: no shebang — nix sandbox lacks /usr/bin/env.
  # base64("hello world\n") = aGVsbG8gd29ybGQK (no embedded newlines)
  cat > "$HOME/bin/gh" <<'GHSTUB'
set -euo pipefail
printf '%s\n' "$@" >> "$HOME/gh-calls"

# Strip --jq <filter> / -f key=val flags from argv, collecting them.
jq_filter=""
new_args=()
skip_next=0
for arg in "$@"; do
  if [ "$skip_next" = "1" ]; then
    if [ "$skip_next_type" = "jq" ]; then
      jq_filter="$arg"
    fi
    skip_next=0
    skip_next_type=""
    continue
  fi
  case "$arg" in
    --jq)   skip_next=1; skip_next_type="jq"; continue ;;
    --jq=*) jq_filter="${arg#--jq=}";           continue ;;
    -f)     skip_next=1; skip_next_type="f";    continue ;;
    -f*)    continue ;;
  esac
  new_args+=("$arg")
done
set -- "${new_args[@]}"

emit() {
  local json="$1"
  if [ -n "$jq_filter" ]; then
    printf '%s' "$json" | jq -r "$jq_filter"
  else
    printf '%s\n' "$json"
  fi
}

case "${2:-}" in
  repos/test-org/test-repo/contents/src/file.txt)
    emit '{"type":"file","sha":"abc123","url":"https://example.com?ref=main","content":"aGVsbG8gd29ybGQK","encoding":"base64"}'
    ;;
  repos/test-org/test-repo/contents/mydir/a.txt)
    emit '{"type":"file","sha":"sha_a","url":"...","content":"aGVsbG8gd29ybGQK","encoding":"base64"}'
    ;;
  repos/test-org/test-repo/contents/mydir/b.txt)
    emit '{"type":"file","sha":"sha_b","url":"...","content":"aGVsbG8gd29ybGQK","encoding":"base64"}'
    ;;
  repos/test-org/test-repo)
    emit '{"default_branch":"main"}'
    ;;
  repos/test-org/test-repo/commits/main)
    emit '{"sha":"deadbeef","commit":{"tree":{"sha":"tree123"}}}'
    ;;
  "repos/test-org/test-repo/git/trees/tree123?recursive=1")
    emit '{"tree":[{"path":"mydir/a.txt","type":"blob","sha":"sha_a"},{"path":"mydir/b.txt","type":"blob","sha":"sha_b"},{"path":"other/c.txt","type":"blob","sha":"sha_c"}]}'
    ;;
  /user)
    emit '"test-user"'
    ;;
  *)
    echo "gh stub: unhandled: $*" >&2
    exit 1
    ;;
esac
GHSTUB
  chmod +x "$HOME/bin/gh"
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

# copy-file: fetches file, decodes base64, writes to dest
function copy_file_fetches_and_writes_file_content_to_dest_path { # @test
  cd "$REPO"
  dest="$HOME/out/file.txt"
  run "$BIN/copy-file" "src/file.txt" "$dest" "" "test-org/test-repo"
  assert_success
  run cat "$dest"
  assert_success
  assert_output "hello world"
}

# copy-file: output JSON includes bytes_written, source_sha, dest_path
function copy_file_output_JSON_has_expected_fields { # @test
  cd "$REPO"
  dest="$HOME/out/file2.txt"
  run "$BIN/copy-file" "src/file.txt" "$dest" "" "test-org/test-repo"
  assert_success
  echo "$output" | jq -e '.bytes_written > 0' || fail '.bytes_written > 0 check failed: '"$output"
  echo "$output" | jq -e '.source_sha == "abc123"' || fail '.source_sha == "abc123" check failed: '"$output"
  echo "$output" | jq -e '.dest_path | endswith("file2.txt")' || fail '.dest_path | endswith("file2.txt") check failed: '"$output"
}

# copy-file: creates parent directories
function copy_file_creates_parent_directories { # @test
  cd "$REPO"
  dest="$HOME/out/deep/nested/file.txt"
  run "$BIN/copy-file" "src/file.txt" "$dest" "" "test-org/test-repo"
  assert_success
  [ -f "$dest" ]
}

# copy-tree: copies only blobs under the src_path prefix
function copy_tree_copies_only_files_under_src_path_preserving_layout { # @test
  cd "$REPO"
  dest="$HOME/out/tree"
  run "$BIN/copy-tree" "mydir" "$dest" "" "test-org/test-repo"
  assert_success
  [ -f "$dest/a.txt" ]
  [ -f "$dest/b.txt" ]
  # other/c.txt is outside mydir and must NOT be copied
  [ ! -f "$HOME/out/other/c.txt" ]
}

# copy-tree: output JSON has files_copied count and paths
function copy_tree_output_JSON_reports_correct_file_count { # @test
  cd "$REPO"
  dest="$HOME/out/tree2"
  run "$BIN/copy-tree" "mydir" "$dest" "" "test-org/test-repo"
  assert_success
  echo "$output" | jq -e '.files_copied == 2' || fail '.files_copied == 2 check failed: '"$output"
  echo "$output" | jq -e '.paths | length == 2' || fail '.paths | length == 2 check failed: '"$output"
  echo "$output" | jq -e '.source_sha == "deadbeef"' || fail '.source_sha == "deadbeef" check failed: '"$output"
}

# copy-write-perms: allows dest inside CWD
function copy_write_perms_allows_dest_inside_CWD { # @test
  cd "$REPO"
  run "$BIN/copy-write-perms" "$REPO/output.txt"
  assert_success
  assert_output --partial "auto-allowed"
}

# copy-write-perms: denies dest in /nix/store
function copy_write_perms_denies_dest_in_nix_store { # @test
  cd "$REPO"
  run "$BIN/copy-write-perms" "/nix/store/abc-foo/bin/file"
  assert_failure
  [ "$status" -eq 2 ]
}

# copy-write-perms: asks for dest outside CWD and write-allow dirs
function copy_write_perms_asks_for_dest_outside_CWD { # @test
  cd "$REPO"
  run "$BIN/copy-write-perms" "/tmp/some-random-path/file.txt"
  [ "$status" -eq 1 ]
  assert_output --partial "confirmation required"
}
