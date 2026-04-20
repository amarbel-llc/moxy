#! /usr/bin/env bats
#
# Contract for the release-tarball flake output. Both sides of the
# contract — the CI upload pipeline and the homebrew-moxy formula's
# install block — depend on the layout this test asserts. When it
# changes, the derivation in flake.nix, .github/workflows/release.yml,
# and Formula/moxy.rb all have to move together.

setup_file() {
  load "$BATS_TEST_DIRNAME/common.bash"

  # Justfile builds .#release-tarball before invoking bats and exports
  # RELEASE_TARBALL_DIR. Extract once per file; both tests share the
  # extracted tree.
  [ -n "${RELEASE_TARBALL_DIR:-}" ] ||
    {
      echo "RELEASE_TARBALL_DIR not set (was build-release-tarball run?)" >&2
      exit 1
    }
  [ -d "$RELEASE_TARBALL_DIR" ] ||
    {
      echo "RELEASE_TARBALL_DIR=$RELEASE_TARBALL_DIR is not a directory" >&2
      exit 1
    }

  export RELEASE_TARBALL
  for candidate in "$RELEASE_TARBALL_DIR"/moxy-*.tar.gz; do
    [ -f "$candidate" ] && RELEASE_TARBALL="$candidate" && break
  done
  [ -f "${RELEASE_TARBALL:-}" ] ||
    {
      echo "no moxy-*.tar.gz in $RELEASE_TARBALL_DIR" >&2
      ls -la "$RELEASE_TARBALL_DIR" >&2
      exit 1
    }

  export RELEASE_EXTRACT
  RELEASE_EXTRACT=$(mktemp -d)
  tar -xzf "$RELEASE_TARBALL" -C "$RELEASE_EXTRACT"
}

teardown_file() {
  [ -n "${RELEASE_EXTRACT:-}" ] && rm -rf "$RELEASE_EXTRACT"
}

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

# ----- Contract: tarball layout -----

function release_tarball_has_bin_moxy { # @test
  [ -f "$RELEASE_EXTRACT/moxy/bin/moxy" ]
  [ -x "$RELEASE_EXTRACT/moxy/bin/moxy" ]
}

function release_tarball_has_moxins_tree { # @test
  # Core moxins that ship in every release. If one disappears, the
  # formula's depends_on list may be wrong — fail loudly here rather
  # than at brew install time.
  local expected=(
    env folio folio-external freud get-hubbed get-hubbed-external
    grit hamster jq just-us-agents man rg sisyphus
  )
  for name in "${expected[@]}"; do
    [ -d "$RELEASE_EXTRACT/moxy/share/moxy/moxins/$name" ] ||
      {
        echo "missing moxin: $name" >&2
        return 1
      }
    [ -f "$RELEASE_EXTRACT/moxy/share/moxy/moxins/$name/_moxin.toml" ] ||
      {
        echo "missing _moxin.toml for $name" >&2
        return 1
      }
  done
}

function release_tarball_has_man_pages { # @test
  [ -f "$RELEASE_EXTRACT/moxy/share/man/man1/moxy.1" ]
  [ -f "$RELEASE_EXTRACT/moxy/share/man/man5/moxyfile.5" ]
  [ -f "$RELEASE_EXTRACT/moxy/share/man/man5/moxy-hooks.5" ]
  [ -f "$RELEASE_EXTRACT/moxy/share/man/man7/moxin.7" ]
}

function release_tarball_moxin_tomls_have_bin_substituted { # @test
  # Per the contract: @BIN@ is resolved to the relative path "bin" at
  # build time (mkBrewMoxin / mkBrewBunMoxin). moxy joins with SourceDir
  # at runtime, so no install-time inreplace is needed. If any @BIN@
  # leaks through, the formula will need updating OR the build-time
  # substitution broke.
  if grep -rFl '@BIN@' "$RELEASE_EXTRACT/moxy/share/moxy/moxins" >/dev/null; then
    echo "Unresolved @BIN@ placeholders in moxin TOMLs:" >&2
    grep -rFl '@BIN@' "$RELEASE_EXTRACT/moxy/share/moxy/moxins" >&2
    return 1
  fi
  # And a positive assertion: at least one moxin TOML uses the relative
  # "bin/…" form that results from the substitution.
  grep -rq '^command = "bin/' "$RELEASE_EXTRACT/moxy/share/moxy/moxins"
}

# ----- Contract: post-install smoke -----

function release_tarball_moxy_serves_moxins_from_extracted_tree { # @test
  # Wire the extracted tree the way the formula is expected to wire
  # it: moxy binary on PATH, MOXIN_PATH pointing at the moxins tree.
  # Then call a moxin tool and verify it works end-to-end.
  export PATH="$RELEASE_EXTRACT/moxy/bin:$PATH"
  export MOXIN_PATH="$RELEASE_EXTRACT/moxy/share/moxy/moxins"

  mkdir -p "$HOME/project"
  echo "hello" >"$HOME/project/file.txt"
  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.ls" \
    '{name: $n, arguments: {path: "."}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | jq -r '.content[0].resource.text' | grep -q "file.txt"
}
