set output-format := "tap"

default: build test

build: build-go build-nix

build-go:
  go build -o build/moxy ./cmd/moxy

build-gomod2nix:
  gomod2nix

build-nix: build-gomod2nix
  nix build --show-trace

test: test-go

test-go:
  go vet ./...
  go test ./... -v

run-nix *ARGS:
  nix run . -- {{ARGS}}

update: update-go

update-go:
  env GOPROXY=direct go get -u -t ./...
  go mod tidy

clean: clean-build

clean-build:
  rm -rf result build/
