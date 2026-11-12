#! /usr/bin/env bats

# bats file_tags=startup_errors

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

# moxy#438: an error returned after stderrlog.Init has teed fd 2 into a pipe
# used to die in that pipe when main called os.Exit, so moxy exited 1 with no
# reason. main now unwinds to a single exit and drains the pipe first, so the
# error reaches the terminal and the session log. An out-of-range
# MOXY_HTTP_ADDR port fails the early bind, which runs after Init, without
# touching the network.
function fatal_startup_error_after_stderr_tee_is_printed { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  MOXY_HTTP_ADDR=127.0.0.1:99999 run_moxy serve mcp
  assert_failure 1
  assert_output --partial "moxy: listening on 127.0.0.1:99999"

  # The session log is created only when Init installs the tee. Rotate has
  # moved it to completed/ by the time the error is printed, but the copier
  # still holds the file open, so the final line and the stopping banner land
  # in it once the pipe is drained.
  local completed="${XDG_LOG_HOME:-$HOME/.local/log}/moxy/stderr/completed"
  run find "$completed" -name '*.log'
  assert_success
  assert_output --regexp '\.log$'
  run grep -rh 'moxy: listening on 127.0.0.1:99999' "$completed"
  assert_success
  run grep -rh -- '--- moxy stopping' "$completed"
  assert_success
}
