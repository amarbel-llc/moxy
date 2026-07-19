{
  inputs = {
    igloo.url = "https://code.linenisgreat.com/igloo/archive/master.tar.gz";
    nixpkgs-master.url = "github:NixOS/nixpkgs/567a49d1913ce81ac6e9582e3553dd90a955875f";
    # Pinned to the last upstream nixpkgs commit where pkgs.gomarkdoc still
    # builds. A regression after 2026-03-23 (still present on master as of
    # 2026-05-04) breaks gomarkdoc's checkPhase — used only as the source of
    # `pkgs.gomarkdoc` for hamster-moxin's @GOMARKDOC@ substitution. Remove
    # this input once NixOS/nixpkgs#516481 lands.
    nixpkgs-gomarkdoc-pin.url = "github:NixOS/nixpkgs/4590696c8693fea477850fe379a01544293ca4e2";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    purse-first = {
      url = "https://code.linenisgreat.com/purse-first/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    tommy = {
      url = "https://code.linenisgreat.com/tommy/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    # amarbel-llc/bats provides bats-libs (the bundled library tree
    # consumed by mkBatsLane's batsLibPath). It used to also provide
    # batman (the sandcastle-wrapping bats wrapper) for the devshell,
    # but #249 moved bats execution into the nix sandbox — the devshell
    # uses pkgs.bats directly now. Used to come via amarbel-llc/bob,
    # but bob was dropped — moxy doesn't depend on anything else it
    # shipped.
    bats = {
      url = "https://code.linenisgreat.com/bats/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.utils.follows = "utils";
    };

    # madder is the content-addressable blob store backing the moxin
    # result cache. Pinned at build time so `moxy version` reports an
    # auditable revision; users can override with
    # `nix build .#moxy --override-input madder github:amarbel-llc/madder/<rev>`.
    madder = {
      url = "https://code.linenisgreat.com/madder/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    # conformist — the linter+formatter multiplexer (formerly treelint; RFC
    # 0001). moxy's formatter + linter config lives in ./conformist.nix, merged
    # with conformist.lib.presets.eng via conformist.lib.evalModule (see the
    # outputs below). The module produces the `nix fmt` wrapper, the read-only
    # `conformist check` gate, and the store-pinned conformist-pre-commit /
    # conformist-repair git hooks — replacing the former treefmt-nix config.
    conformist = {
      url = "https://code.linenisgreat.com/conformist/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    # clown ships the ringmaster job-control binary (RFC-0015). moxy pins it
    # so the async producers (internal/asyncjob, the get-hubbed ci-watch moxin)
    # run a hermetic, version-locked ringmaster rather than resolving it off
    # ambient PATH. Referenced as clown.packages.${system}.ringmaster and baked
    # in via ldflag + wrapper (see the moxy package and get-hubbed-moxin below).
    #
    # Interim per the job-platform extraction plan: clown's provider inputs
    # (llm-agents, the claude-code/codex/llama nixpkgs pins, treefmt-nix) still
    # enter moxy's flake.lock because moxy has nothing to `follows` them onto —
    # only the shared inputs below dedup. When the lightweight job-platform
    # flake lands this becomes a one-line input swap to that repo.
    clown = {
      url = "https://code.linenisgreat.com/clown/archive/master.tar.gz";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.bats.follows = "bats";
    };

    # linenisgreat fork of forgejo-cli (fj), carrying the FDR-0016
    # vanity-discovery patches for owner-less vanity remotes
    # (code.linenisgreat.com). Its outputs function uses a closed parameter set
    # ({ self, nixpkgs-master, utils, conformist }, no ...) — do not add
    # follows for inputs it doesn't declare. (Its former `nixpkgs` input was
    # renamed nixpkgs-master in forgejo-cli ebdedb8; conformist added in
    # 56c6de7.)
    forgejo-cli = {
      url = "https://code.linenisgreat.com/forgejo-cli/archive/master.tar.gz";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.conformist.follows = "conformist";
    };

    madder.inputs.bats.follows = "bats";
    tommy.inputs.bats.follows = "bats";
    clown.inputs.conformist.follows = "conformist";
    madder.inputs.conformist.follows = "conformist";
    purse-first.inputs.conformist.follows = "conformist";
    tommy.inputs.conformist.follows = "conformist";
    utils.inputs.systems.follows = "igloo/systems";
    tommy.inputs.tap.follows = "madder/tap";
    bats.inputs.nixpkgs-master.follows = "nixpkgs-master";
    igloo.inputs.nixpkgs-master.follows = "nixpkgs-master";
    madder.inputs.purse-first.follows = "purse-first";
    madder.inputs.tommy.follows = "tommy";
    bats.inputs.conformist.follows = "conformist";
  };

  outputs =
    {
      self,
      igloo,
      nixpkgs-master,
      nixpkgs-gomarkdoc-pin,
      utils,
      purse-first,
      tommy,
      bats,
      madder,
      conformist,
      clown,
      forgejo-cli,
    }:
    (utils.lib.eachDefaultSystem (
      system:
      let
        # The amarbel-llc/nixpkgs fork auto-applies its own overlay,
        # which carries a patched buildGoApplication that auto-injects
        # `-X main.version` and `-X main.commit` ldflags. Re-applying
        # `gomod2nix.overlays.default` from nix-community/gomod2nix
        # would shadow the patched version with the upstream one and
        # silently drop the auto-injection — see madder's go/default.nix.
        pkgs = import igloo { inherit system; };

        pkgs-master = import nixpkgs-master {
          inherit system;
        };

        # See nixpkgs-gomarkdoc-pin in inputs above.
        pkgs-gomarkdoc-pin = import nixpkgs-gomarkdoc-pin {
          inherit system;
        };

        pkgs-master-unfree = import nixpkgs-master {
          inherit system;
          config.allowUnfreePredicate =
            pkg:
            builtins.elem (pkgs.lib.getName pkg) [
              "acli"
              "acli-unwrapped"
            ];
        };

        conformistPkg = conformist.packages.${system}.conformist;

        # The dead-jq bats linter (scripts/lint-dead-jq), packaged via
        # conformist's sandbox-safe helper (conformist#19): it patchShebangs the
        # `#!/usr/bin/env bash` script so it execs inside the pure-nix check
        # sandbox, then --prefix-wraps PATH with runtimeInputs (a superset of
        # the old --set PATH). This is the [linter.dead-jq] command in
        # ./conformist.nix.
        deadJqChecker = conformist.lib.writeCheckScript pkgs {
          name = "lint-dead-jq";
          src = ./scripts/lint-dead-jq;
          runtimeInputs = [
            pkgs.gawk
            pkgs.gnugrep
          ];
        };

        # Lenient mypy type-check for the first-party moxin Python (#10). The
        # python env bundles mypy with types-requests so the `requests` stub
        # resolves; mypy.ini at the tree root supplies ignore_missing_imports +
        # scripts_are_modules. Wrapped the same way as deadJqChecker and wired as
        # the [linter.mypy] command in ./conformist.nix; also exposed as
        # packages.lint-py-types for the debug-py-typecheck dev-loop.
        mypyEnv = pkgs.python3.withPackages (ps: [
          ps.mypy
          ps.types-requests
        ]);
        pyTypesChecker = conformist.lib.writeCheckScript pkgs {
          name = "lint-py-types";
          src = ./scripts/lint-py-types;
          runtimeInputs = [
            mypyEnv
            pkgs.gnugrep
            pkgs.coreutils
          ];
        };

        # moxy's formatter + linter config, merged with the eng preset. The
        # pure eval (presets.eng + ./conformist.nix) drives `nix fmt`
        # (build.wrapper), the read-only gate (build.check), and the
        # store-pinned git hooks (build.preCommit / build.repair). tommy (TOML
        # formatter) and the two custom check scripts are store-path deps built
        # here, injected into the module via _module.args — same pattern moxy
        # used to inject tommy into treefmt. See ./conformist.nix.
        conformistEval = conformist.lib.evalModule pkgs {
          imports = [
            conformist.lib.presets.eng
            ./conformist.nix
          ];
          package = conformistPkg;
          _module.args = {
            inherit deadJqChecker pyTypesChecker;
            tommy = tommy.packages.${system}.default;
          };
        };

        # Impure git-state lane: the eng-convention checks that need a live .git
        # or host tools (git-remotes, git-default-branch, sweatfile's `spinclass
        # validate`, agents-md, gomod2nix). They can't run in the sandboxed
        # gate, so this config drives a working-tree `conformist check` via
        # `just lint-worktree`. See the eng-impure preset and conformist-nix(7).
        conformistImpureEval = conformist.lib.evalModule pkgs {
          imports = [ conformist.lib.presets.eng-impure ];
          package = conformistPkg;
          projectRootFile = "flake.nix";
        };

        moxySrc = pkgs.lib.fileset.toSource {
          root = ./.;
          fileset =
            with pkgs.lib.fileset;
            unions [
              ./go.mod
              ./go.sum
              ./gomod2nix.toml
              ./cmd/moxy
              ./internal
              # Consumed by the fork's buildGoApplication version.env
              # auto-read (nixpkgs#31): the read is at pwd (= moxySrc), so
              # the file must survive into the filtered tree or the
              # embedded version falls back to the gomod2nix placeholder.
              ./version.env
            ];
        };

        # flake-input-go_mod bridge (amarbel-llc/nixpkgs RFC 0001). Routes the
        # two amarbel-llc Go modules moxy directly imports onto their producer
        # flakes' `go-pkgs` source trees, so a tommy/go-mcp bump is a flake.lock
        # change alone — no go.mod pseudo-version + gomod2nix.toml hash lockstep.
        # The bridge synthesizes `replace` directives at eval time and strips
        # these keys from the merged gomod2nix.toml, so go.mod/gomod2nix.toml are
        # left untouched (their real require versions still drive out-of-nix
        # `go build`). madder is NOT bridged: it's a binary input, not imported
        # Go code. Consumed by both buildGoApplication and the devshell mkGoEnv.
        goFlakeInputs = {
          "github.com/amarbel-llc/tommy" = tommy.packages.${system}.go-pkgs;
          "github.com/amarbel-llc/purse-first/libs/go-mcp" = {
            src = purse-first.packages.${system}.go-pkgs;
            subPath = "libs/go-mcp";
          };
        };

        # The google-workspace (gws) moxins — gmail, calendar, car, gws, piers,
        # prison — are excluded from the build closure for now (#391): they need
        # the `gws` OAuth CLI most users lack, so they'd fail on probe / clutter
        # the default tool surface. Commented out (not deleted) pending the
        # proper disabled-by-default opt-in mechanism (#391). To restore:
        # uncomment this block, the gwsDeps + moxin defs below, and their
        # moxy-moxins symlinks.
        /*
            # google-workspace-cli release pin (a third-party binary, not moxy's own
            # version). Named `gwsRelease`, NOT `gwsVersion`: the eng-versioning
          # deprecated-file linter flags any `*Version = "<semver>"` let-binding in
          # flake.nix (it wants moxy's version in version.env), and this is an
          # unrelated vendored-tool pin.
          gwsRelease = "0.22.5";
          gwsPlatform =
            {
              "aarch64-darwin" = {
                name = "aarch64-apple-darwin";
                hash = "sha256-HSqf/VvJssLEtIYw2vCC+tE9nlfXQZiKLCSO7VYvfaw=";
              };
              "x86_64-darwin" = {
                name = "x86_64-apple-darwin";
                hash = "sha256-Ufm9cxQE1LuibDbi4w3WjFbczR+DTAElLLCxTWplRLI=";
              };
              "x86_64-linux" = {
                name = "x86_64-unknown-linux-gnu";
                hash = "sha256-3njs29LxqEzKAGOn7LxEAkD8FLbrzLsX9GRreSqMXB8=";
              };
              "aarch64-linux" = {
                name = "aarch64-unknown-linux-gnu";
                hash = "sha256-lEkCldlYDh6IV05xWgoWKZF0fRLWL4x7jcyCaLbBzqA=";
              };
            }
            .${system} or (throw "gws: unsupported system ${system}");

          gws-bin = pkgs.stdenv.mkDerivation {
            pname = "gws";
            version = gwsRelease;
            src = pkgs.fetchurl {
              url = "https://github.com/googleworkspace/cli/releases/download/v${gwsRelease}/google-workspace-cli-${gwsPlatform.name}.tar.gz";
              hash = gwsPlatform.hash;
            };
            sourceRoot = ".";
            installPhase = ''
              mkdir -p $out/bin
              cp gws $out/bin/gws
              chmod +x $out/bin/gws
            '';
          };
        */

        # version.env at repo root is the single source of truth for the
        # release version. The moxy Go binary gets it for free via the
        # fork's buildGoApplication version.env auto-read (nixpkgs#31) —
        # version.env is part of moxySrc. This eval-time binding remains
        # for the *non-Go* consumers that have no auto-injection path:
        # the bun binaries, the zx scripts, and the plugin.json version
        # field. `just bump-version` sed-rewrites version.env. Match
        # captures everything after `MOXY_VERSION=` up to the line break.
        moxyVersion = builtins.head (
          builtins.match ".*MOXY_VERSION=([^\n]+).*" (builtins.readFile ./version.env)
        );
        # shortRev for clean builds, dirtyShortRev for dirty working
        # trees (so devshell builds show `dirty-abcdef` rather than
        # masquerading as a clean release), "unknown" as a last-resort
        # fallback.
        moxyCommit = self.shortRev or self.dirtyShortRev or "unknown";

        # Man pages as a standalone derivation, referenced by both the moxy
        # binary package and the man moxin (avoids circular dependency).
        moxy-man =
          pkgs.runCommand "moxy-man"
            {
              nativeBuildInputs = [ pkgs.man-db ];
            }
            ''
              mkdir -p $out/share/man/man1 $out/share/man/man5 $out/share/man/man7
              cp ${./cmd/moxy/moxy.1} $out/share/man/man1/moxy.1
              cp ${./cmd/moxy/moxyfile.5} $out/share/man/man5/moxyfile.5
              cp ${./cmd/moxy/moxy-hooks.5} $out/share/man/man5/moxy-hooks.5
              cp ${./cmd/moxy/moxin.7} $out/share/man/man7/moxin.7
              cp ${./cmd/moxy/moxy-restart.7} $out/share/man/man7/moxy-restart.7
              cp ${./cmd/moxy/moxy-batch.7} $out/share/man/man7/moxy-batch.7
              MANPATH=$out/share/man mandb --no-purge --create $out/share/man
            '';

        # Helper: build a moxin with bin/ scripts wrapped with PATH deps.
        # extraSubstitutions: attrset of {NAME = "/abs/path";} pairs. Each
        # `@NAME@` placeholder anywhere under the moxin tree is replaced with
        # the literal value via `substitute`, so resolved store paths are
        # baked into the scripts at build time — same convention as `@BIN@`
        # and as mkBunMoxin's extraSubstitutions.
        mkMoxin =
          name: deps:
          {
            pathMode ? "set",
            extraWrapArgs ? [ ],
            extraSubstitutions ? { },
          }:
          pkgs.runCommand "${name}-moxin"
            {
              nativeBuildInputs = [ pkgs.makeWrapper ] ++ deps;
            }
            (
              ''
                cp -r ${./moxins/${name}} $out
                chmod -R u+w $out
                chmod +x $out/bin/*
                # Rewrite each script's `#!/usr/bin/env <prog>` shebang to an
                # absolute /nix/store path. The strict bats sandbox does not
                # expose /usr/bin/env, so env-resolved shebangs would fail with
                # "bad interpreter: No such file or directory". patchShebangs
                # uses interpreters from nativeBuildInputs, so deps are added
                # there above.
                patchShebangs $out/bin
                ${
                  if pathMode == "inherit" && extraWrapArgs == [ ] then
                    ''
                      # pathMode=inherit with no extra args: skip wrapProgram entirely so
                      # scripts run with the host's unmodified environment.
                    ''
                  else
                    ''
                      for f in $out/bin/*; do
                        wrapProgram "$f" \
                          ${
                            if pathMode != "inherit" then
                              "--${pathMode} PATH ${if pathMode == "set" then "" else ": "}${pkgs.lib.makeBinPath deps}"
                            else
                              ""
                          } \
                          --unset LD_LIBRARY_PATH \
                          ${pkgs.lib.concatStringsSep " " extraWrapArgs}
                      done
                    ''
                }
                for f in $(grep -rl '@BIN@' $out); do
                  substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
                done
              ''
              + pkgs.lib.concatMapStringsSep "\n" (placeholder: ''
                for f in $(grep -rl "@${placeholder}@" $out 2>/dev/null || true); do
                  substitute "$f" "$f" --replace-fail "@${placeholder}@" "${extraSubstitutions.${placeholder}}"
                done
              '') (builtins.attrNames extraSubstitutions)
            );

        # Helper: build a moxin that has bun+zx compiled scripts in src/.
        # extraSubstitutions: attrset of {NAME = "/abs/path";} pairs.
        # Before bundling, each `@NAME@` placeholder in the moxin's TS source
        # is replaced with the literal value via `substitute`, so the bundler
        # bakes the resolved store path directly into the JS — no runtime
        # env-var or PATH indirection. Same convention as `@BIN@` elsewhere.
        mkBunMoxin =
          name: deps: extraEntrypoints:
          {
            pathMode ? "set",
            extraWrapArgs ? [ ],
            extraSubstitutions ? { },
          }:
          let
            rawSrc = pkgs.lib.fileset.toSource {
              root = ./.;
              fileset =
                with pkgs.lib.fileset;
                unions [
                  ./moxins/${name}/src
                  ./package.json
                  ./bun.lock
                ];
            };
            src =
              if extraSubstitutions == { } then
                rawSrc
              else
                pkgs.runCommand "${name}-moxin-src" { } (
                  ''
                    cp -rL ${rawSrc} $out
                    chmod -R u+w $out
                  ''
                  + pkgs.lib.concatMapStringsSep "\n" (placeholder: ''
                    for f in $(grep -rl "@${placeholder}@" $out 2>/dev/null || true); do
                      substitute "$f" "$f" --replace-fail "@${placeholder}@" "${extraSubstitutions.${placeholder}}"
                    done
                  '') (builtins.attrNames extraSubstitutions)
                );
            bunBinaries = pkgs.buildBunBinaries {
              pname = "${name}-moxin-scripts";
              version = moxyVersion;
              inherit src;
              bunNix = ./bun.nix;
              entrypoints = extraEntrypoints;
              runtimeInputs = deps;
              # Emit inline sourcemaps so bun backtraces show original TS
              # source locations instead of minified bundle offsets (#270).
              bunBuildFlags = [ "--sourcemap=inline" ];
            };
          in
          pkgs.runCommand "${name}-moxin"
            {
              nativeBuildInputs = [ pkgs.makeWrapper ] ++ deps;
            }
            ''
              cp -r ${./moxins/${name}} $out
              chmod -R u+w $out
              rm -rf $out/src
              mkdir -p $out/bin
              for f in $out/bin/*; do [ -e "$f" ] && chmod +x "$f"; done
              # Rewrite `#!/usr/bin/env <prog>` shebangs in cp'd source scripts to
              # absolute /nix/store paths, same as mkMoxin. The bats sandbox lacks
              # /usr/bin/env, so env-resolved shebangs would fail. The bun wrappers
              # written below already use an absolute bash path; this catches the
              # source scripts that came in via `cp -r`.
              if [ -n "$(ls -A $out/bin 2>/dev/null)" ]; then
                patchShebangs $out/bin
              fi
              # Create wrapper scripts that locate the bundled JS files.
              # buildBunBinaries wrappers assume flat output, but bun >=9
              # entrypoints may nest them. We extract the bundle store path
              # and search for each JS file in both layouts.
              bundle_dir=""
              for f in ${bunBinaries}/bin/*; do
                bundle_dir=$(grep -oE '/nix/store/[^/]+' "$f" | grep bundle | head -1)
                [ -n "$bundle_dir" ] && break
              done
              for f in ${bunBinaries}/bin/*; do
                binname=$(basename "$f")
                jsfile="$binname.js"
                if [ -f "$bundle_dir/$jsfile" ]; then
                  js_path="$bundle_dir/$jsfile"
                else
                  js_path=$(find "$bundle_dir" -name "$jsfile" -type f | head -1)
                fi
                if [ -z "$js_path" ]; then
                  echo "ERROR: could not find $jsfile in $bundle_dir" >&2
                  exit 1
                fi
                bun_bin=$(grep -oE '/nix/store/[^ ]+/bin/bun' "$f" | head -1)
                cat > "$out/bin/$binname" <<WRAPPER
              #!${pkgs.bash}/bin/bash
              exec $bun_bin $js_path "\$@"
              WRAPPER
                chmod +x "$out/bin/$binname"
              done
              for f in $out/bin/*; do
                wrapProgram "$f" \
                  ${
                    if pathMode != "inherit" then
                      "--${pathMode} PATH ${if pathMode == "set" then "" else ": "}${pkgs.lib.makeBinPath deps}"
                    else
                      ""
                  } \
                  --unset LD_LIBRARY_PATH \
                  ${pkgs.lib.concatStringsSep " " extraWrapArgs}
              done
              for f in $(grep -rl '@BIN@' $out); do
                substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
              done
            '';

        # --- arboretum tree-sitter grammar wasm, built from source ----------
        #
        # The grammars used to be hand-copied binaries from tree-sitter-wasms
        # (moxins/arboretum/wasm/, ABI 14) with no reproducible pipeline — and
        # that vendored bash grammar crashed web-tree-sitter on `case`
        # statements (moxy#379). Now every grammar is built from a pinned
        # source rev against a tree-sitter CLI matching the runtime, so the
        # grammar version is derived (not a frozen blob) and fresh builds emit
        # an ABI the runtime accepts. The vendored moxins/arboretum/wasm/ dir
        # is gone — this derivation is the sole source of the wasm.
        #
        # tree-sitter 0.26's `build --wasm` compiles via wasi-sdk's clang. By
        # default it DOWNLOADS wasi-sdk at build time (network — hostile to a
        # hermetic build); loader.rs checks TREE_SITTER_WASI_SDK_PATH first, so
        # we fetch the prebuilt sdk as an FOD and point that env var at it. The
        # output wasm is platform-independent, but the build needs the sdk for
        # the *builder* platform — hence the per-system map.
        wasiSdkVersion = "29.0";
        wasiSdkBySystem = {
          aarch64-darwin = {
            arch = "arm64-macos";
            hash = "sha256-4RVSkT4/meg01/59ob0IGrr3ZHWe12tgl6NMY/yDZl4=";
          };
          x86_64-darwin = {
            arch = "x86_64-macos";
            hash = "sha256-0N4v0+pcVwYO+ofkNWwWS+w2iZcvI4bwyaicWOEM7I0=";
          };
          aarch64-linux = {
            arch = "arm64-linux";
            hash = "sha256-BSrXczl9yeWqmftM/vaUF15rHoG7KtHTyOez/IFEG3w=";
          };
          x86_64-linux = {
            arch = "x86_64-linux";
            hash = "sha256-h9HRooedE5zcYkuWjvrT1Kl7gHjN/5XmOsiOyv0aAXE=";
          };
        };
        wasiSdkInfo =
          wasiSdkBySystem.${system} or (throw "arboretum-grammars: no wasi-sdk pinned for ${system}");
        wasiSdkTarball = pkgs.fetchurl {
          url = "https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-${builtins.head (pkgs.lib.splitString "." wasiSdkVersion)}/wasi-sdk-${wasiSdkVersion}-${wasiSdkInfo.arch}.tar.gz";
          hash = wasiSdkInfo.hash;
        };

        # The prebuilt wasi-sdk release ships dynamically-linked host binaries:
        # its clang's ELF interpreter is /lib64/ld-linux-x86-64.so.2, a path the
        # Nix build sandbox lacks, so `tree-sitter build --wasm` can't exec it
        # (execve → ENOENT, surfaced as "Failed to run wasi-sdk clang"). Extract
        # the SDK once and, on Linux, run it through autoPatchelfHook so clang +
        # its shared libs point at the Nix glibc loader. On Darwin the binaries
        # are Mach-O with no interpreter indirection, so no patching is needed —
        # which is why the from-source path only broke once ea54cee turned it on
        # for Linux. The whole SDK tree is copied (not just bin/clang) so the
        # CFGDIR-relative paths in bin/clang.cfg still resolve to the sysroot.
        # (#388)
        wasiSdk = pkgs.stdenv.mkDerivation {
          name = "wasi-sdk-${wasiSdkVersion}";
          src = wasiSdkTarball;
          nativeBuildInputs = pkgs.lib.optional pkgs.stdenv.isLinux pkgs.autoPatchelfHook;
          buildInputs = pkgs.lib.optionals pkgs.stdenv.isLinux [
            pkgs.stdenv.cc.cc.lib # libstdc++ / libgcc_s for clang
            pkgs.zlib # libz
            pkgs.ncurses # libtinfo for clang
          ];
          # Stay in the build root so the unpacked wasi-sdk-*/ dir is a child
          # (default unpack auto-cd's into it, which breaks the glob copy).
          sourceRoot = ".";
          dontBuild = true;
          installPhase = ''
            mkdir -p $out
            cp -a wasi-sdk-*/. $out/
          '';
        };

        # tree-sitter grammar sources, pinned by rev. ts and tsx share one
        # repo (two subdir grammars); php's grammar lives in the `php/` subdir
        # (vs `php_only/`). Each entry maps a wasm OUTPUT name to its source +
        # the subdir within that source to build (default ".").
        tsGrammarSrc =
          owner: repo: rev: hash:
          pkgs.fetchFromGitHub {
            inherit
              owner
              repo
              rev
              hash
              ;
          };
        tsTypescriptSrc =
          tsGrammarSrc "tree-sitter" "tree-sitter-typescript" "75b3874edb2dc714fb1fd77a32013d0f8699989f"
            "sha256-A0M6IBoY87ekSV4DfGHDU5zzFWdLjGqSyVr6VENgA+s=";
        treeSitterGrammars = {
          bash = {
            src =
              tsGrammarSrc "tree-sitter" "tree-sitter-bash" "a06c2e4415e9bc0346c6b86d401879ffb44058f7"
                "sha256-ONQ1Ljk3aRWjElSWD2crCFZraZoRj3b3/VELz1789GE=";
          };
          go = {
            src =
              tsGrammarSrc "tree-sitter" "tree-sitter-go" "2346a3ab1bb3857b48b29d779a1ef9799a248cd7"
                "sha256-fifTM/m2Mxd7kpJBlzwWGheAKGq6QbbzyxpBSyplYa0=";
          };
          rust = {
            src =
              tsGrammarSrc "tree-sitter" "tree-sitter-rust" "77a3747266f4d621d0757825e6b11edcbf991ca5"
                "sha256-Ls6tB6IxXDQDWwx0BJ7RgbheelC4MH8z97E7wwhkDcY=";
          };
          python = {
            src =
              tsGrammarSrc "tree-sitter" "tree-sitter-python" "26855eabccb19c6abf499fbc5b8dc7cc9ab8bc64"
                "sha256-gHeja/X/Ux8fa5rh0b69/bcUcmHBcXsK5uJ1ibtuI20=";
          };
          javascript = {
            src =
              tsGrammarSrc "tree-sitter" "tree-sitter-javascript" "58404d8cf191d69f2674a8fd507bd5776f46cb11"
                "sha256-+fbTNX7qz6Ew1NrXF49wQh3RVl2ZQ3R7YXMkclUoNT8=";
          };
          typescript = {
            src = tsTypescriptSrc;
            subdir = "typescript";
          };
          tsx = {
            src = tsTypescriptSrc;
            subdir = "tsx";
          };
          php = {
            src =
              tsGrammarSrc "tree-sitter" "tree-sitter-php" "38216983c07bf9e1b56e16acde53b25adaeab61c"
                "sha256-Y02akiL95WGV8J3gd6FXQ0XHPoE59d2zuFQkXh6eyAQ=";
            subdir = "php";
          };
        };

        # The runtime wasm web-tree-sitter loads at Parser.init (outline.ts /
        # abi-check.ts ask for "tree-sitter.wasm" via locateFile). Source it
        # from the SAME web-tree-sitter version pinned in bun.lock (0.25.10) so
        # the runtime wasm name + version track the runtime — closing the third
        # drift dimension (the file was renamed across a web-tree-sitter major).
        webTreeSitterWasm = pkgs.fetchurl {
          url = "https://registry.npmjs.org/web-tree-sitter/-/web-tree-sitter-0.25.10.tgz";
          hash = "sha256-ZjZFespeaUX7c2cTYq4ZycTlE7tRalPVjEorTRJTPg8=";
        };

        # Build one grammar's wasm. Output: $out/tree-sitter-<name>.wasm.
        buildGrammarWasm =
          name: spec:
          pkgs.stdenv.mkDerivation {
            name = "tree-sitter-${name}-wasm";
            src = spec.src;
            nativeBuildInputs = [
              pkgs.tree-sitter
            ];
            buildPhase = ''
              export HOME=$TMPDIR
              export TREE_SITTER_WASI_SDK_PATH=${wasiSdk}
              mkdir -p $out
              tree-sitter build --wasm \
                -o $out/tree-sitter-${name}.wasm \
                ${spec.subdir or "."}
            '';
            dontInstall = true;
            dontFixup = true;
          };

        builtGrammarWasms = pkgs.lib.mapAttrs buildGrammarWasm treeSitterGrammars;

        # The WASM_DIR the arboretum moxin loads from, built entirely from
        # source: every grammar wasm + the runtime tree-sitter.wasm sourced
        # from the pinned web-tree-sitter. No vendored binaries.
        arboretumWasmDir = pkgs.runCommand "arboretum-wasm" { } ''
          mkdir -p $out
          tar -xzf ${webTreeSitterWasm} -C $out --strip-components=1 \
            package/tree-sitter.wasm
          ${pkgs.lib.concatStringsSep "\n" (
            pkgs.lib.mapAttrsToList (name: drv: "cp ${drv}/tree-sitter-${name}.wasm $out/") builtGrammarWasms
          )}
        '';

        # Per-moxin derivations — each moxin is self-contained with its deps.
        # @WASM_DIR@ resolves at build time to the arboretum wasm dir (built
        # grammars overlaid on the vendored runtime/grammar set). Same
        # convention as hamster's @GOMARKDOC@/@PANDOC@: pre-bundle substitution
        # bakes the absolute store path into outline.js so the bundled JS can
        # locate both web-tree-sitter's runtime wasm and the language grammars
        # without any PATH or env-var indirection.
        arboretum-moxin =
          mkBunMoxin "arboretum"
            [
              pkgs.bash
              pkgs.ast-grep
              pkgs.pandoc
            ]
            {
              "outline" = "moxins/arboretum/src/outline.ts";
              "search" = "moxins/arboretum/src/search.ts";
              "rewrite" = "moxins/arboretum/src/rewrite.ts";
              # Drift gate: asserts vendored grammar ABIs are in the runtime's
              # supported range + the runtime wasm exists (moxy#379).
              "abi-check" = "moxins/arboretum/src/abi-check.ts";
              # md-* tools shell out to pandoc for markdown AST work. Same gfm
              # reader the (now-retired) pandoc moxin used.
              "md-toc" = "moxins/arboretum/src/md-toc.ts";
              "md-section" = "moxins/arboretum/src/md-section.ts";
              "md-anchor" = "moxins/arboretum/src/md-anchor.ts";
            }
            {
              extraSubstitutions = {
                WASM_DIR = "${arboretumWasmDir}";
              };
            };
        # chix uses pathMode = "suffix" so the user's nix binary wins (and
        # picks up the user's NIX_PATH / config), while manix + git + the
        # shell helpers are guaranteed to resolve from the wrapper's
        # PATH suffix when the ambient environment doesn't provide them.
        # Needed for chix.doc* (manix) and chix.flake-update / flake-lock
        # (nix shells out to git to stage the updated lock file).
        chix-moxin =
          mkBunMoxin "chix"
            [
              pkgs.bash
              pkgs.coreutils
              pkgs.findutils
              pkgs.git
              pkgs.gnugrep
              pkgs.jq
              pkgs.manix
            ]
            {
              "flake-check" = "moxins/chix/src/flake-check.ts";
              "flake-lock" = "moxins/chix/src/flake-lock.ts";
              "flake-show" = "moxins/chix/src/flake-show.ts";
              "flake-update" = "moxins/chix/src/flake-update.ts";
              "store-ls" = "moxins/chix/src/store-ls.ts";
            }
            { pathMode = "suffix"; };
        env-moxin =
          mkMoxin "env"
            [
              pkgs.bash
              pkgs.coreutils
              pkgs.jq
              pkgs.which
            ]
            {
              pathMode = "suffix";
              # clock resolves IANA zone files via TZDIR; pin tzdata so the
              # timezone-convert path works without any host zoneinfo (#340).
              extraWrapArgs = [
                "--set"
                "TZDIR"
                "${pkgs.tzdata}/share/zoneinfo"
              ];
            };
        folio-moxin = mkMoxin "folio" [
          pkgs.bash
          pkgs.coreutils
          pkgs.file
          pkgs.findutils
          pkgs.gawk
          pkgs.git # folio-perms resolves the repo's main worktree to allow sibling-repo reads
          pkgs.gnugrep
          pkgs.gnutar
          pkgs.gzip
          pkgs.jq
          pkgs.tree
        ] { };
        freud-moxin = mkMoxin "freud" [ pkgs.python3 ] { };
        # pathMode = "suffix" so user PATH wins (and can shadow gh with a
        # stub in tests). ci-watch resolves the ringmaster job-control CLI
        # (clown RFC-0015) at runtime via RINGMASTER_BIN; --set-default pins the
        # hermetic store path while still letting a test-provided RINGMASTER_BIN
        # win (the bats ci_watch lane injects a stub through it).
        get-hubbed-moxin =
          mkBunMoxin "get-hubbed"
            [
              pkgs.bash
              pkgs.coreutils
              pkgs.gawk
              pkgs.git
              pkgs-master.gh
              pkgs.jq
              pkgs.util-linux
            ]
            {
              "issue-get" = "moxins/get-hubbed/src/issue-get.ts";
              "issue-list" = "moxins/get-hubbed/src/issue-list.ts";
              "issue-transfer" = "moxins/get-hubbed/src/issue-transfer.ts";
              "content-compare" = "moxins/get-hubbed/src/content-compare.ts";
              "ci-watch" = "moxins/get-hubbed/src/ci-watch.ts";
            }
            {
              pathMode = "suffix";
              extraWrapArgs = [
                "--set-default"
                "RINGMASTER_BIN"
                "${clown-ringmaster}/bin/ringmaster"
              ];
            };
        # grit deliberately uses pathMode = "inherit" with no nix-pinned deps:
        # it must run the user's own git (matching the repo they're operating
        # on, with their configured aliases/templates/credential helpers). Under
        # "inherit", mkBunMoxin's wrapProgram skips the PATH arg entirely, so a
        # deps list here would be a no-op — both the bun binary and the bash
        # scripts resolve git through the inherited process PATH at exec time.
        # Keep this empty; see #219.
        grit-moxin = mkBunMoxin "grit" [ ] {
          "push-stack" = "moxins/grit/src/push-stack.ts";
        } { pathMode = "inherit"; };
        # @GOMARKDOC@ / @PANDOC@ are baked into the bundled JS at build time
        # via mkBunMoxin's extraSubstitutions, so the markdown renderer path
        # doesn't depend on the user's PATH or any runtime env var. doc.ts
        # carries a PATH-fallback for non-nix builds (brew, devshell) where
        # the placeholders survive into the bundle unmodified.
        hamster-moxin =
          mkBunMoxin "hamster"
            [
              pkgs.bash
              pkgs.coreutils
              pkgs.findutils
              pkgs.gawk
              pkgs.gnused
              pkgs.jq
              pkgs-master.go_1_26
            ]
            {
              "doc" = "moxins/hamster/src/doc.ts";
              "doc-outline" = "moxins/hamster/src/doc-outline.ts";
              "src" = "moxins/hamster/src/src.ts";
              "mod-read" = "moxins/hamster/src/mod-read.ts";
            }
            {
              pathMode = "inherit";
              extraSubstitutions = {
                # gomarkdoc pulled from a pinned older nixpkgs (see the
                # nixpkgs-gomarkdoc-pin input) because the version in
                # current nixpkgs has a broken checkPhase.
                GOMARKDOC = "${pkgs-gomarkdoc-pin.gomarkdoc}/bin/gomarkdoc";
                PANDOC = "${pkgs.pandoc}/bin/pandoc";
              };
            };
        sisyphus-python = pkgs.python3.withPackages (ps: [
          ps.atlassian-python-api
          # Sole runtime dep of vendored marklas (moxins/sisyphus/lib/_vendor/marklas).
          ps.mistune
        ]);
        # @PANDOC@ in moxins/sisyphus/lib/_lib.py is baked in at build time
        # so the read-side ADF→Markdown post-process doesn't depend on the
        # user's PATH. The Lua filter path is resolved at runtime relative
        # to _lib.py's location (it's a sibling file), so it doesn't need
        # a substitution.
        sisyphus-moxin = mkMoxin "sisyphus" [ sisyphus-python pkgs.bash pkgs.jq ] {
          extraSubstitutions = {
            PANDOC = "${pkgs.pandoc}/bin/pandoc";
          };
        };
        jq-moxin = mkMoxin "jq" [ pkgs.bash pkgs.jq ] { };
        just-us-agents-moxin =
          let
            listRecipes = pkgs.buildZxScript {
              pname = "just-list-recipes";
              version = moxyVersion;
              src = ./moxins/just-us-agents/src;
              entrypoint = "list-recipes.ts";
              runtimeInputs = [ ];
            };
          in
          pkgs.runCommand "just-us-agents-moxin"
            {
              nativeBuildInputs = [
                pkgs.makeWrapper
                pkgs.bash
              ];
            }
            ''
              cp -r ${./moxins/just-us-agents} $out
              chmod -R u+w $out
              rm -rf $out/src
              mkdir -p $out/bin
              for f in $out/bin/*; do [ -e "$f" ] && chmod +x "$f"; done
              # Rewrite `#!/usr/bin/env bash` shebangs to absolute /nix/store paths
              # before wrapping. The bats sandbox lacks /usr/bin/env, so an
              # env-resolved shebang on the .run-recipe-wrapped script fails with
              # "bad interpreter: No such file or directory". Same step mkMoxin and
              # mkBunMoxin perform; this hand-rolled derivation must do it too.
              patchShebangs $out/bin
              ln -sf ${listRecipes}/bin/just-list-recipes $out/bin/list-recipes
              for f in $out/bin/*; do
                [ -L "$f" ] && continue
                wrapProgram "$f" --unset LD_LIBRARY_PATH
              done
              for f in $(grep -rl '@BIN@' $out); do
                substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
              done
            '';
        man-moxin =
          mkMoxin "man"
            [
              pkgs.bash
              pkgs.coreutils
              pkgs.gawk
              pkgs.gnugrep
              pkgs.gzip
              pkgs.man-db
              pkgs.mandoc
              pkgs.manix
              pkgs.pandoc
            ]
            {
              extraWrapArgs = [
                # Default MANPATH to moxy's own man dir followed by a trailing
                # colon. Per manpath(5), a trailing colon makes man-db append
                # its *determined* default search path (man_db.conf + the
                # PATH-derived dirs, incl. the user's home-manager profile and
                # system pages) after moxy's bundled set.
                #
                # Why --set-default and not --prefix/--suffix: makeWrapper's
                # --prefix/--suffix UNCONDITIONALLY strip leading & trailing
                # separators from the result (see make-shell-wrapper-hook), so
                # they can never emit the trailing-colon empty entry man-db
                # needs. With MANPATH unset (the normal runtime case under moxy)
                # the old `--prefix MANPATH : <dir>` therefore produced a single
                # authoritative entry and man-db never derived the
                # profile/system set — which is why `man spinclass` / `man eng-*`
                # failed through this moxin while the user's shell found them.
                # Only --set / --set-default write the value verbatim (colon
                # preserved); --set-default is chosen over --set so a caller that
                # HAS set MANPATH wins (the bats sandbox exports `$HOME/man:` to
                # reach its pivy-tool fixture — --set would clobber it).
                #
                # Edge note: --set-default uses `${MANPATH-...}` (`-`, not `:-`),
                # so a set-but-EMPTY MANPATH ("") is left empty rather than
                # defaulted. The moxy process always has MANPATH unset (defaulted
                # here) and the bats sandbox sets it non-empty, so neither path
                # hits that case.
                "--set-default"
                "MANPATH"
                "${moxy-man}/share/man:"
              ];
              pathMode = "suffix";
            };
        rg-moxin = mkMoxin "rg" [ pkgs.bash pkgs-master.ripgrep ] { };
        smith-moxin = mkMoxin "smith" [
          pkgs.bash
          pkgs.coreutils
          pkgs.gawk
          pkgs.jq
          forgejo-cli.packages.${system}.default
        ] { };

        # gws moxins excluded from the build closure for now (#391) — see the
        # commented gws-bin block above. Restore by uncommenting.
        /*
            gwsDeps = [
            pkgs.bash
            pkgs.coreutils
            gws-bin
          ];
          piers-moxin = mkBunMoxin "piers" gwsDeps {
            "get" = "moxins/piers/src/get.ts";
            "create" = "moxins/piers/src/create.ts";
            "update" = "moxins/piers/src/update.ts";
            "batch-update" = "moxins/piers/src/batch-update.ts";
            "replace-text" = "moxins/piers/src/replace-text.ts";
            "insert-text" = "moxins/piers/src/insert-text.ts";
            "delete-content-range" = "moxins/piers/src/delete-content-range.ts";
            "update-text-style" = "moxins/piers/src/update-text-style.ts";
            "update-paragraph-style" = "moxins/piers/src/update-paragraph-style.ts";
            "comments-list" = "moxins/piers/src/comments-list.ts";
            "comment-reply" = "moxins/piers/src/comment-reply.ts";
            "comment-resolve" = "moxins/piers/src/comment-resolve.ts";
            "outline" = "moxins/piers/src/outline.ts";
            "tab-create" = "moxins/piers/src/tab-create.ts";
            "tab-delete" = "moxins/piers/src/tab-delete.ts";
            "tab-update" = "moxins/piers/src/tab-update.ts";
          } { };
          car-moxin = mkBunMoxin "car" gwsDeps {
            "search" = "moxins/car/src/search.ts";
            "get" = "moxins/car/src/get.ts";
            "list" = "moxins/car/src/list.ts";
            "export" = "moxins/car/src/export.ts";
            "doc-graph" = "moxins/car/src/doc-graph.ts";
          } { };
        */
        slip-moxin = pkgs.runCommand "slip-moxin" { } ''
          cp -r ${./moxins/slip} $out
        '';
        /*
            prison-moxin = mkBunMoxin "prison" gwsDeps {
            "get" = "moxins/prison/src/get.ts";
          } { };
          gmail-moxin = mkBunMoxin "gmail" gwsDeps {
            "triage" = "moxins/gmail/src/triage.ts";
            "read" = "moxins/gmail/src/read.ts";
          } { };
          calendar-moxin = mkBunMoxin "calendar" gwsDeps {
            "agenda" = "moxins/calendar/src/agenda.ts";
          } { };
          gws-moxin = mkBunMoxin "gws" gwsDeps {
            "api" = "moxins/gws/src/api.ts";
          } { };
        */

        # Symlink-only aggregation of all per-moxin derivations.
        moxy-moxins = pkgs.runCommand "moxy-moxins" { } ''
          mkdir -p $out/share/moxy/moxins
          ln -s ${arboretum-moxin} $out/share/moxy/moxins/arboretum
          ln -s ${chix-moxin} $out/share/moxy/moxins/chix
          ln -s ${env-moxin} $out/share/moxy/moxins/env
          ln -s ${folio-moxin} $out/share/moxy/moxins/folio
          ln -s ${freud-moxin} $out/share/moxy/moxins/freud
          ln -s ${get-hubbed-moxin} $out/share/moxy/moxins/get-hubbed
          ln -s ${grit-moxin} $out/share/moxy/moxins/grit
          ln -s ${hamster-moxin} $out/share/moxy/moxins/hamster
          ln -s ${sisyphus-moxin} $out/share/moxy/moxins/sisyphus
          ln -s ${smith-moxin} $out/share/moxy/moxins/smith
          ln -s ${jq-moxin} $out/share/moxy/moxins/jq
          ln -s ${just-us-agents-moxin} $out/share/moxy/moxins/just-us-agents
          ln -s ${man-moxin} $out/share/moxy/moxins/man
          ln -s ${rg-moxin} $out/share/moxy/moxins/rg
          ln -s ${slip-moxin} $out/share/moxy/moxins/slip
          # gws moxins (piers car prison gmail calendar gws) excluded for now (#391).
        '';

        madder-bin = madder.packages.${system}.default;

        # ringmaster (clown RFC-0015 job-control CLI). Its absolute store path
        # is baked into the moxy binary (asyncjob ldflag) and the get-hubbed
        # ci-watch moxin (RINGMASTER_BIN wrapper) so async producers run a
        # hermetic, pinned ringmaster instead of relying on ambient PATH.
        clown-ringmaster = clown.packages.${system}.ringmaster;

        moxy = pkgs.buildGoApplication {
          pname = "moxy";
          commit = moxyCommit;
          src = moxySrc;
          # pwd lets the goFlakeInputs merge read the consumer go.mod (mirrors
          # madder's src+pwd pairing).
          pwd = moxySrc;
          inherit goFlakeInputs;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          nativeBuildInputs = [
            pkgs.makeWrapper
            pkgs.jq
          ];
          # The fork's buildGoApplication auto-injects `-X main.version`
          # (read from the version.env carried in src/pwd, nixpkgs#31)
          # and `-X main.commit` (from the `commit` attr above). Only
          # project-specific ldflags need to live here.
          ldflags = [
            "-X"
            "github.com/amarbel-llc/moxy/internal/native.defaultSystemMoxinDir=${moxy-moxins}/share/moxy/moxins"
            "-X"
            "github.com/amarbel-llc/moxy/internal/native.defaultMadderBin=${madder-bin}/bin/madder"
            # Pin the ringmaster job-control CLI (clown RFC-0015) as the async
            # producer's default, so wakeups don't depend on ambient PATH. The
            # RINGMASTER_BIN env var still overrides this (tests/pinning).
            "-X"
            "github.com/amarbel-llc/moxy/internal/asyncjob.defaultRingmasterBin=${clown-ringmaster}/bin/ringmaster"
          ];
          postInstall = ''
            MOXY_MCP_BINARY="$out/bin/moxy" $out/bin/moxy generate-plugin $out

            # purse-first's generate-plugin doesn't emit a `version` field;
            # inject `<semver>+<commit>` so the distributed plugin.json
            # matches what `moxy --version` reports (semver build-metadata
            # is the suffix after `+`). Claude Code plugins spec requires a
            # semver `version`, and `+commit` is valid SemVer 2.0.0
            # build-metadata.
            pluginJson="$out/share/purse-first/moxy/.claude-plugin/plugin.json"
            jq --arg v "${moxyVersion}+${moxyCommit}" '.version = $v' "$pluginJson" > "$pluginJson.tmp"
            mv "$pluginJson.tmp" "$pluginJson"

            # clown-plugin-protocol manifest for HTTP MCP transport.
            substitute ${./clown.json} $out/share/purse-first/moxy/clown.json \
              --replace-fail "@MOXY@" "$out/bin/moxy"

            # Static hooks — go-mcp's GenerateHooks no-ops (no MapsTools),
            # so we install them at the correct plugin path.
            mkdir -p $out/share/purse-first/moxy/hooks
            cp ${./hooks/hooks.json} $out/share/purse-first/moxy/hooks/hooks.json
            substitute ${./hooks/pre-tool-use} $out/share/purse-first/moxy/hooks/pre-tool-use \
              --replace-fail "@MOXY@" "$out/bin/moxy"
            chmod +x $out/share/purse-first/moxy/hooks/pre-tool-use

            cp -rn ${moxy-man}/share/man/* $out/share/man/

            # Moxin tools have their own PATH via wrapProgram in per-moxin
            # derivations. The moxy binary itself only needs bash for
            # process management.
            wrapProgram $out/bin/moxy \
              --prefix PATH : ${
                pkgs.lib.makeBinPath [
                  pkgs.bash
                ]
              }
          '';
        };

        combined = pkgs.symlinkJoin {
          name = "moxy";
          # pname is consulted by `batsLane` for lane derivation naming;
          # symlinkJoin doesn't set it by default, so spell it out.
          pname = "moxy";
          paths = [
            moxy
            moxy-moxins
          ];
        };

        # --- Apple `container` prototype (Nix -> OCI Linux image) ---------
        #
        # Apple's `container` (nixpkgs `container`) runs *Linux* containers as
        # per-container lightweight VMs and consumes standard OCI Linux images.
        # There is no macOS-userland container; the prototype is "Nix builds a
        # Linux OCI image of moxy, `container` runs it". See the
        # `container-prototype` justfile recipe.
        #
        # moxy is pure Go (CGO off), so Go's own cross-compiler emits the
        # aarch64-linux binary with no Linux build VM and no nixpkgs cross set.
        #
        # The mechanism: gomod2nix's buildGoApplication takes the target from
        # the `go` package itself (`inherit (go) GOOS GOARCH`, builder
        # default.nix:507), NOT from passed GOOS/GOARCH attrs. So the cross
        # target is selected by handing it a `go` whose GOOS/GOARCH passthru say
        # linux/arm64 — while the binary on PATH stays the *native* (Darwin) Go,
        # which cross-compiles fine via those env vars. Staying on the native
        # `pkgs` (not pkgsCross) is deliberate: pkgsCross trips an igloo bug
        # where mkMergedGoMod puts `go` in buildInputs (target-platform, not on
        # PATH under cross) → "go: command not found" (filed upstream).
        #
        # CGO stays off so no C toolchain is needed. The Darwin-only postInstall
        # (plugin/hook/man wiring + `wrapProgram` baking a Darwin bash path) is
        # dropped — the image just needs `$out/bin/moxy`. The full moxy package
        # (moxins + maneater's CGo llama-cpp) does NOT cross-compile and is out
        # of scope for this prototype.
        goLinuxArm64 = pkgs-master.go_1_26 // {
          GOOS = "linux";
          GOARCH = "arm64";
        };
        moxy-linux = pkgs.buildGoApplication {
          pname = "moxy";
          commit = moxyCommit;
          src = moxySrc;
          pwd = moxySrc;
          inherit goFlakeInputs;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = goLinuxArm64;
          GOTOOLCHAIN = "local";
          CGO_ENABLED = "0";
          # The check phase runs `go test`, which can't exec a linux/arm64
          # test binary on the darwin builder ("exec format error"). Tests run
          # natively in the gate; skip them for the cross artifact.
          doCheck = false;
          # Cross builds nest the binary under bin/<goos>_<goarch>/ (Go's
          # GOOS/GOARCH-suffixed output dir). Flatten it back to bin/moxy so the
          # image entrypoint /bin/moxy resolves. Unconditional: if Go ever stops
          # nesting, the mv should fail loudly rather than silently no-op.
          postInstall = ''
            mv "$out/bin/linux_arm64/moxy" "$out/bin/moxy"
            rmdir "$out/bin/linux_arm64"
          '';
        };

        # Layered image assembled by streamLayeredImage (pure-Nix, runs on
        # Darwin); only the *contents* are the cross-built aarch64-linux
        # closure. `architecture` is set explicitly because the build host is
        # darwin and the default would pick up hostPlatform. This is the
        # Docker-format archive (manifest.json + layer dirs) — an intermediate;
        # Apple's `container image load` wants OCI layout, so it's converted
        # below.
        moxy-docker-image = pkgs.dockerTools.buildLayeredImage {
          name = "moxy";
          tag = "latest";
          architecture = "arm64";
          contents = [ moxy-linux ];
          # Entrypoint (not Cmd) so `container run moxy:latest <args>` appends
          # args to the moxy binary rather than treating the first arg as the
          # executable to run. With a bare Cmd, `container run moxy:latest
          # --version` tries to exec `--version`.
          config.Entrypoint = [ "/bin/moxy" ];
        };

        # `container image load` rejects the Docker-format archive
        # (`oci-layout` not found). skopeo converts the docker-archive into an
        # oci-archive tarball (oci-layout + index.json + blobs/) that
        # `container` accepts. skopeo runs natively on Darwin — this is a pure
        # format transcode, no Linux execution. The result is a single
        # `moxy-oci.tar` the recipe loads directly.
        moxy-oci-image =
          pkgs.runCommand "moxy-oci.tar"
            {
              nativeBuildInputs = [ pkgs.skopeo ];
            }
            ''
              skopeo --insecure-policy copy \
                docker-archive:${moxy-docker-image} \
                oci-archive:$out:moxy:latest
            '';

        # Bats integration test source tree, fed to `batsLane` to run the
        # suite inside the nix build sandbox. See #249 for the
        # batman/sandcastle interaction this replaces.
        batsSrc = pkgs.lib.fileset.toSource {
          root = ./zz-tests_bats;
          fileset =
            with pkgs.lib.fileset;
            unions [
              ./zz-tests_bats/common.bash
              ./zz-tests_bats/test-fixtures
              ./zz-tests_bats/test-permission-request-hook.mjs
              (fileFilter (f: f.hasExt "bats") ./zz-tests_bats)
            ];
        };

        # batsLane was formerly `pkgs.testers.batsLane`, shipped by the
        # amarbel-llc/nixpkgs fork overlay. The builder moved into the
        # amarbel-llc/bats flake so it tracks bats releases rather than
        # nixpkgs rebases (see amarbel-llc/bats — `lib.${system}.batsLane`).
        batsLane = bats.lib.${system}.batsLane;

        # Helper for building a single bats lane against the combined
        # moxy + moxy-moxins symlinkJoin (so the binary's baked-in
        # defaultSystemMoxinDir resolves and madder/MOXIN_PATH wiring
        # is consistent with what real users see). Mirrors madder's
        # go/default.nix:40-54 pattern.
        mkBatsLane =
          {
            filter ? "!net_cap,!host_only",
            base ? combined,
          }:
          batsLane {
            inherit base filter batsSrc;
            binaries = {
              MOXY_BIN = {
                inherit base;
                name = "moxy";
              };
              MADDER_BIN = {
                base = madder-bin;
                name = "madder";
              };
            };
            batsLibPath = [ bats.packages.${system}.bats-libs.batsLibPath ];
            extraEnv = {
              BATS_TEST_TIMEOUT = "30";
              MOXIN_PATH = "${moxy-moxins}/share/moxy/moxins";
              # grit_*.bats invoke wrapped scripts at $BIN by default
              # ($BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin),
              # which doesn't exist inside the nix sandbox. Tests fall
              # back to GRIT_BIN via ${GRIT_BIN:-$BIN}.
              GRIT_BIN = "${grit-moxin}/bin";
              # chix_*.bats invoke wrapped scripts via ${CHIX_BIN:-$BIN},
              # which doesn't exist inside the nix sandbox.
              CHIX_BIN = "${chix-moxin}/bin";
              # get_hubbed_*.bats invoke wrapped scripts via ${GET_HUBBED_BIN:-$BIN},
              # which doesn't exist inside the nix sandbox.
              GET_HUBBED_BIN = "${get-hubbed-moxin}/bin";
              # freud_tool_usage.bats falls back to FREUD_BIN (wrapped
              # python script) when set; otherwise invokes python3
              # directly against the source tree (devshell path).
              FREUD_BIN = "${freud-moxin}/bin/tool-usage";
              # sisyphus_*.bats invoke wrapped scripts via ${SISYPHUS_BIN:-$BIN},
              # which doesn't exist inside the nix sandbox.
              SISYPHUS_BIN = "${sisyphus-moxin}/bin";
              # man_*.bats invoke wrapped scripts via ${MAN_BIN:-$BIN},
              # which doesn't exist inside the nix sandbox.
              MAN_BIN = "${man-moxin}/bin";
              # smith.bats invokes wrapped scripts via ${SMITH_BIN:-$BIN},
              # which doesn't exist inside the nix sandbox.
              SMITH_BIN = "${smith-moxin}/bin";
              # just_us_agents_*.bats invoke wrapped scripts via ${JUST_US_AGENTS_BIN:-$BIN},
              # which doesn't exist inside the nix sandbox.
              JUST_US_AGENTS_BIN = "${just-us-agents-moxin}/bin";
              # env_*.bats invoke wrapped scripts via ${ENV_BIN:-$BIN},
              # which doesn't exist inside the nix sandbox.
              ENV_BIN = "${env-moxin}/bin";
            };
            nativeBuildInputs = [
              pkgs.bash
              pkgs.coreutils-full
              pkgs.curl
              pkgs.findutils
              pkgs.git
              pkgs.gnugrep
              pkgs.gnused
              pkgs.gnutar
              pkgs.gawk
              pkgs.gzip
              pkgs.jq
              pkgs.man-db
              # python3 needed by freud_tool_usage.bats's bare
              # `python3 ../moxins/freud/bin/tool-usage` invocation,
              # until the test rewrite uses ${FREUD_BIN} exclusively.
              pkgs.python3
            ];
          };

        # Per-tag bats lane outputs — auto-discovered from
        # `# bats file_tags=<tag>` directives in zz-tests_bats/*.bats.
        # Walk the bats source tree at flake-eval time, collect unique
        # tags, and produce one `bats-${tag}` derivation per tag plus
        # special `bats-default` (filters !net_cap,!host_only),
        # `bats-net_cap`, and `bats-host_only` lanes.
        batsTags =
          let
            dir = ./zz-tests_bats;
            files = builtins.attrNames (
              pkgs.lib.filterAttrs (n: t: t == "regular" && pkgs.lib.hasSuffix ".bats" n) (builtins.readDir dir)
            );
            extract =
              name:
              let
                m = builtins.match ".*# bats file_tags=([a-zA-Z0-9_,.-]+).*" (builtins.readFile (dir + "/${name}"));
              in
              if m == null then [ ] else pkgs.lib.splitString "," (builtins.head m);
          in
          pkgs.lib.unique (pkgs.lib.flatten (map extract files));

        batsLaneOutputs =
          pkgs.lib.listToAttrs (
            map (
              t:
              pkgs.lib.nameValuePair "bats-${t}" (mkBatsLane {
                filter = t;
              })
            ) batsTags
          )
          // {
            bats-default = mkBatsLane { };
            bats-net_cap = mkBatsLane { filter = "net_cap"; };
            bats-host_only = mkBatsLane { filter = "host_only"; };
          };

        # Hermetic Go test + vet checks (#347, folds #348). A lean base off
        # `moxy`: doCheck on, the plugin-gen postInstall dropped (irrelevant to
        # a test run), MOXIN_PATH unset to match `just run-go-test`. The build
        # sandbox gives each a fresh HOME/TMPDIR, so env-dependent tests (e.g.
        # discovery's MOXIN_PATH/HOME fallback) are isolated by construction —
        # the class that leaked into the merge hook. buildGoRace flips
        # CGO_ENABLED=1 + -race for the race deriv only, leaving the shipped
        # binary untouched.
        goCheckBase = moxy.overrideAttrs (_: {
          doCheck = true;
          postInstall = "";
          preCheck = "export MOXIN_PATH=";
        });
        goTestRace = pkgs.buildGoRace { base = goCheckBase; };
        goVet = goCheckBase.overrideAttrs (_: {
          pname = "moxy-govet";
          checkPhase = ''
            runHook preCheck
            go vet ./...
            runHook postCheck
          '';
        });
        # golangci-lint as a hermetic check. moxy's .golangci.yml uses only the
        # standard built-in analyzers (no external plugins), so it typechecks
        # offline against the buildGoApplication module graph. Caches go to
        # $TMPDIR (the sandbox HOME is read-only). golangci-lint from
        # pkgs-master matches the devshell `lint-go` binary (config schema v2).
        goLint = goCheckBase.overrideAttrs (old: {
          pname = "moxy-golangci-lint";
          nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [
            pkgs-master.golangci-lint
          ];
          # --config points at the flake's copy of .golangci.yml: the dotfile
          # is not in the filtered moxySrc, so without this golangci-lint runs
          # with defaults (errcheck on, no exclusions) and flags ~50 idiomatic
          # sites the repo config deliberately suppresses. Path-based
          # exclusions still resolve against the analyzed paths (cwd-relative).
          checkPhase = ''
            runHook preCheck
            export HOME="$TMPDIR"
            export GOLANGCI_LINT_CACHE="$TMPDIR/golangci-lint-cache"
            golangci-lint run --config ${./.golangci.yml} --timeout 10m ./...
            runHook postCheck
          '';
        });

      in
      {
        packages = batsLaneOutputs // {
          inherit
            moxy
            moxy-moxins
            moxy-linux
            moxy-oci-image
            ;
          default = combined;
          # The wrapped lenient-mypy checker (#10), exposed so the
          # debug-py-typecheck recipe runs the exact same binary the gate's
          # [linter.mypy] uses.
          lint-py-types = pyTypesChecker;
          # The store-pinned, toolchain-hermetic git hooks from the pure
          # conformist eval (conformist#47/#51/#54): conformist-pre-commit runs
          # `conformist --staged --exit-zero-on-fix`, conformist-repair runs
          # `conformist --commit --amend --exit-zero-on-fix`. On the devShell
          # PATH under these names; moxy's sweatfile [hooks] names them. Every
          # formatter's command is store-pinned in the baked config, so they
          # cannot silently skip a file type the ambient PATH happens to lack.
          conformist-pre-commit = conformistEval.config.build.preCommit;
          conformist-repair = conformistEval.config.build.repair;
          # The impure-lane config (eng-impure preset). `just lint-worktree`
          # runs `conformist check` against the working tree with this config
          # to exercise the git-state linters (git-remotes, git-default-branch,
          # sweatfile, agents-md, gomod2nix) the sandboxed gate cannot.
          conformist-impure-config = conformistImpureEval.config.build.configFile;
        };

        # `nix fmt` runs the conformist wrapper in repair mode (every formatter
        # from ./conformist.nix + presets.eng, plus any linter repair actions).
        # `checks.formatting` is the sandboxed read-only gate (built by `just
        # lint-fmt`, and evaluated by `nix flake check`): it runs `conformist
        # check`, covering the formatters, the dead-jq + mypy linters, and the
        # eng-preset conventions (eng-versioning, flake-*, justfile-*). Named
        # `formatting` (not `conformist`) to match the fleet convention every
        # other conformist adopter uses for this check.
        formatter = conformistEval.config.build.wrapper;
        # `nix flake check` is the single hermetic gate: conformist (fmt +
        # dead-jq + mypy), the Go test (-race) / vet / golangci-lint checks, and the
        # comprehensive `bats-default` lane (every test except the net_cap and
        # host_only tags, which need sandbox capabilities a flake check can't
        # grant — those stay as their own `nix build` recipes). The per-tag
        # lanes remain in `packages` for focused `just run-bats-tag` runs;
        # bats-default already covers them in aggregate, so re-listing each as
        # a check would only double-build.
        checks = {
          formatting = conformistEval.config.build.check self;
          go-test-race = goTestRace;
          go-vet = goVet;
          go-lint = goLint;
          bats = batsLaneOutputs.bats-default;
        };

        devShells.default = pkgs-master.mkShell {
          packages = [
            # mkGoEnv (RFC 0001 consumer parity) puts the gomod2nix-regen `go`
            # wrapper + gomod2nix CLI on PATH and gives `nix develop` the same
            # goFlakeInputs-merged module graph as `nix build` for nix-driven
            # go work. Replaces the bare go_1_26 + gomod2nix entries.
            (pkgs.mkGoEnv {
              pwd = ./.;
              inherit goFlakeInputs;
            })
            pkgs-master.delve
            pkgs-master.gofumpt
            pkgs-master.golangci-lint
            pkgs-master.golines
            pkgs-master.gopls
            pkgs-master.gotools
            pkgs-master.govulncheck
            pkgs.just
            pkgs.manix
            pkgs.man-db
            pkgs.mandoc
            pkgs.pandoc
            pkgs.ripgrep
            # Pinned inputs for deterministic bats man-page tests. Without
            # these, `manpath(1)` falls back to whatever man pages the host
            # environment provides (Ubuntu 22.04 system jq 1.6 on CI vs
            # whatever the developer has on PATH locally), and the two
            # produce different TOC structures.
            #
            # Note: pkgs.coreutils does NOT include man pages — coreutils-full
            # is the variant that ships them. pkgs.jq splits its man page
            # into a separate `man` output.
            pkgs.coreutils-full
            pkgs.jq
            # Vanilla bats — we used to pull bats.packages.${system}.batman
            # (the sandcastle-wrapping wrapper), but #249 moved the suite
            # to batsLane (`just run-bats`). Devshell needs only the raw
            # bats binary now, for the self-contained
            # `explore-poc-test-dynamic-perms` recipe; the test suite itself
            # runs through nix lanes.
            pkgs.bats
            purse-first.packages.${system}.purse-first
            tommy.packages.${system}.default
            # The raw conformist runner (`nix fmt` / lint-worktree) plus the
            # config-specific, toolchain-hermetic git hooks on PATH under the
            # names moxy's sweatfile references (conformist#47/#51/#54). The
            # pre-commit hook is auto-installed per-worktree by spinclass at
            # `sc start`/`sc resume` from the sweatfile.
            conformistPkg
            conformistEval.config.build.preCommit
            conformistEval.config.build.repair
          ];
        };
      }
    ));
}
