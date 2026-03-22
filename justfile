default: build test

build: build-go build-nix

build-go:
  go build -o build/moxy ./cmd/moxy

build-gomod2nix:
  gomod2nix

build-nix: build-gomod2nix
  nix build --show-trace

dir_build := "build"

test: test-go test-bats test-validate-mcp

test-bats: build-go
  just --set bin_dir {{justfile_directory()}}/{{dir_build}} zz-tests_bats/test

test-bats-file file: build-go
  just --set bin_dir {{justfile_directory()}}/{{dir_build}} zz-tests_bats/test-targets {{file}}

test-go:
  go vet ./...
  go test ./... -v

test-validate-mcp: build-go
  purse-first validate-mcp {{justfile_directory()}}/{{dir_build}}/moxy

mcp-inspect := "npx @modelcontextprotocol/inspector --cli"

test-mcp: build-go
  #!/usr/bin/env nix
  #! nix shell nixpkgs#nodejs --command bash
  set -euo pipefail
  tools=$({{mcp-inspect}} --method tools/list {{justfile_directory()}}/{{dir_build}}/moxy)
  echo "$tools" | jq .

run-nix *ARGS:
  nix run . -- {{ARGS}}

update: update-go

update-go:
  env GOPROXY=direct go get -u -t ./...
  go mod tidy

clean: clean-build

clean-build:
  rm -rf result build/
