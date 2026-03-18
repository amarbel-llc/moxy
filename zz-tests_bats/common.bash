bats_load_library bats-support
bats_load_library bats-assert
bats_load_library bats-assert-additions
bats_load_library bats-island

run_moxy() {
  run timeout --preserve-status "5s" moxy "$@"
}
