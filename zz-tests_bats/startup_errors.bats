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
# reason. An out-of-range MOXY_HTTP_ADDR port fails the early bind, which runs
# after Init, without touching the network.
function fatal_startup_error_after_stderr_tee_is_printed { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  MOXY_HTTP_ADDR=127.0.0.1:99999 run_moxy serve mcp
  assert_failure 1
  assert_output --partial "moxy: listening on 127.0.0.1:99999"

  # Init creates the session log only when it installs the tee, and Rotate
  # moves it to completed/ synchronously, so the file shows this run went
  # through the pipe the error used to be lost in. (The banners are no proof:
  # they travel through that pipe too and may not be drained before exit.)
  run find "${XDG_LOG_HOME:-$HOME/.local/log}/moxy/stderr/completed" -name '*.log'
  assert_success
  assert_output --regexp '\.log$'
}
