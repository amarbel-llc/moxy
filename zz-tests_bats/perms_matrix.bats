#! /usr/bin/env bats

# bats file_tags=perms_matrix

# moxy#496: `moxy perms-matrix` renders every moxin tool with its permission
# tier. The ungated path readers it flags are pinned here, so adding an
# always-allow tool with a path-like input and no [resources] stanza fails
# this lane until someone decides it (add a stanza, change the tier, or add it
# to this list on purpose).

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

# docs/plans/2026-11-03-permissions-audit.md says what the smallest change is
# for each of these. grit.diff, grit.log and grit.status (moxy#510) are listed
# on purpose: their `paths` are pathspecs inside the session's own repository
# (no repo_path, and git refuses a pathspec outside it), not file reads.
EXPECTED_UNGATED=(
  chix.build
  chix.derivation-show
  chix.flake-check
  chix.flake-init
  chix.flake-lock
  chix.flake-update
  chix.log
  chix.run
  chix.store-cat
  chix.store-ls
  chix.store-path-info
  chix.store-realize
  env.get
  env.which
  freud.messages
  freud.subagents
  freud.summary
  freud.tool-usage
  freud.transcript
  freud.unique-tools
  grit.commit
  grit.diff
  grit.log
  grit.merge
  grit.status
  man.section
  man.toc
)

flagged_tools() {
  "${MOXY_BIN:-moxy}" perms-matrix |
    sed -n '/^## Ungated path readers/,$p' |
    sed -n 's/^- `\([^`]*\)`:.*/\1/p'
}

function the_matrix_lists_the_gated_readers_with_their_stanza { # @test
  run "${MOXY_BIN:-moxy}" perms-matrix
  [ "$status" -eq 0 ]
  [[ $output == *'| `folio.read` | dynamic | broker+floor |'* ]]
  [[ $output == *'| `jq.jq` | dynamic | broker+floor |'* ]]
  [[ $output == *'| `folio.df` | dynamic | broker+floor |'* ]]
  [[ $output == *'| `toolbox.run` | dynamic | broker+floor |'* ]]
  [[ $output == *'| `env.clock` | always-allow | none |'* ]]
}

function the_ungated_path_readers_are_exactly_the_decided_list { # @test
  run flagged_tools
  [ "$status" -eq 0 ]
  expected=$(printf '%s\n' "${EXPECTED_UNGATED[@]}")
  [ "$output" = "$expected" ] || {
    echo "ungated path readers changed:" >&2
    diff <(echo "$expected") <(echo "$output") >&2 || true
    false
  }
}

function a_gated_reader_is_never_flagged { # @test
  run flagged_tools
  [[ $output != *folio.read* ]]
  [[ $output != *jq.jq* ]]
  [[ $output != *toolbox.run* ]]
}
