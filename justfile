set output-format := "tap"

default: build test

build: build-go

build-go:
  go build -o build/moxy ./cmd/moxy

test: test-go

test-go:
  go vet ./...

run-nix *ARGS:
  nix run . -- {{ARGS}}

update: update-go

update-go:
  env GOPROXY=direct go get -u -t ./...
  go mod tidy

clean: clean-build

clean-build:
  rm -rf result build/
