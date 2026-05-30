#!/usr/bin/env bats

# bats file_tags=lint

# Repo-level lint guards over the bats suite itself. These run in the default
# lane (the `lint` tag is neither net_cap nor host_only) and as
# `just test-bats-tag lint`. Both guards defend against ARG_MAX (E2BIG) blowups
# caused by stuffing large captured $output into a process's argv or envp.

load 'common'

# Regression guard for #284: a setup() that does `export output` (or exports
# any other large captured value) puts response bodies — e.g. a full
# tools/list payload — into the process environment. Once envp crosses
# ARG_MAX, every later exec() in that shell fails with E2BIG ("Argument list
# too long"), as awk/rm did in streamable_http.bats. $output is only ever read
# in-shell, so it must never be exported.
function no_bats_file_exports_output { # @test
  local files=("$BATS_TEST_DIRNAME"/*.bats)
  # Guard against a vacuous pass: if the glob matched nothing, the check
  # scanned no files and would silently "pass" forever.
  [ -e "${files[0]}" ] || {
    echo "no .bats files found under $BATS_TEST_DIRNAME — lint guard is vacuous" >&2
    return 1
  }

  local hits
  hits=$(grep -En '^[[:space:]]*export[[:space:]]+output[[:space:]]*$' \
    "${files[@]}" || true)
  if [ -n "$hits" ]; then
    echo "Found 'export output' — an ARG_MAX env-bloat bomb (see #284)." >&2
    echo "Use \$output in-shell; do not export it:" >&2
    echo "$hits" >&2
    return 1
  fi
}

# Sibling hazard to the above, but via argv instead of envp: a line like
#   run bash -c "echo '$output' | jq -e '...'"
# expands the captured body into bash's argument list, so a large $output
# overflows ARG_MAX (and embedded quotes break the command). Feed it through
# stdin instead: `run jq -e 'FILTER' <<<"$output"`.
function no_bats_file_inlines_captured_output_into_bash_c { # @test
  local files=("$BATS_TEST_DIRNAME"/*.bats)
  [ -e "${files[0]}" ] || {
    echo "no .bats files found under $BATS_TEST_DIRNAME — lint guard is vacuous" >&2
    return 1
  }

  # Matches the hazardous `run bash -c "echo ...` idiom. The safe
  # positional-arg form (`run bash -c '...' -- "$a" "$b"`) uses single quotes
  # and does not start with echo, so it is not flagged.
  local hits
  hits=$(grep -En '^[[:space:]]*run[[:space:]]+bash[[:space:]]+-c[[:space:]]+"echo' \
    "${files[@]}" || true)
  if [ -n "$hits" ]; then
    echo "Found captured output inlined into a bash -c argv (ARG_MAX hazard)." >&2
    echo "Feed it via stdin instead: run jq -e 'FILTER' <<<\"\$output\":" >&2
    echo "$hits" >&2
    return 1
  fi
}
