#!/usr/bin/env bats

# bats file_tags=lint

# Repo-level lint guards over the bats suite itself. These run in the default
# lane (the `lint` tag is neither net_cap nor host_only) and as
# `just test-bats-tag lint`.

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
