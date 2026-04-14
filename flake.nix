{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/4590696c8693fea477850fe379a01544293ca4e2";
    nixpkgs-master.url = "github:NixOS/nixpkgs/e2dde111aea2c0699531dc616112a96cd55ab8b5";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    purse-first = {
      url = "github:amarbel-llc/purse-first";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    tommy = {
      url = "github:amarbel-llc/tommy";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    maneater = {
      url = "github:amarbel-llc/maneater";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    bob = {
      url = "github:amarbel-llc/bob";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    bun = {
      url = "github:amarbel-llc/bun";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-master,
      utils,
      gomod2nix,
      purse-first,
      tommy,
      maneater,
      bob,
      bun,
    }:
    (utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            gomod2nix.overlays.default
          ];
        };

        pkgs-master = import nixpkgs-master {
          inherit system;
        };

        pkgs-master-unfree = import nixpkgs-master {
          inherit system;
          config.allowUnfreePredicate = pkg: builtins.elem (pkgs.lib.getName pkg) [ "acli" "acli-unwrapped" ];
        };

        moxySrc = pkgs.lib.fileset.toSource {
          root = ./.;
          fileset = with pkgs.lib.fileset; unions [
            ./go.mod
            ./go.sum
            ./gomod2nix.toml
            ./cmd/moxy
            ./internal
          ];
        };

        bunLib = bun.lib.mkBunLib { inherit pkgs; };

        # Man pages as a standalone derivation, referenced by both the moxy
        # binary package and the man moxin (avoids circular dependency).
        moxy-man = pkgs.runCommand "moxy-man" {
          nativeBuildInputs = [ pkgs.man-db ];
        } ''
          mkdir -p $out/share/man/man1 $out/share/man/man5 $out/share/man/man7
          cp ${./cmd/moxy/moxy.1} $out/share/man/man1/moxy.1
          cp ${./cmd/moxy/moxyfile.5} $out/share/man/man5/moxyfile.5
          cp ${./cmd/moxy/moxy-hooks.5} $out/share/man/man5/moxy-hooks.5
          cp ${./cmd/moxy/moxin.7} $out/share/man/man7/moxin.7
          MANPATH=$out/share/man mandb --no-purge --create $out/share/man
        '';

        # Helper: build a moxin with bin/ scripts wrapped with PATH deps.
        mkMoxin = name: deps: { pathMode ? "set", extraWrapArgs ? [] }: pkgs.runCommand "${name}-moxin" {
          nativeBuildInputs = [ pkgs.makeWrapper ];
        } ''
          cp -r ${./moxins/${name}} $out
          chmod -R u+w $out
          chmod +x $out/bin/*
          for f in $out/bin/*; do
            wrapProgram "$f" \
              ${if pathMode != "inherit" then "--${pathMode} PATH ${if pathMode == "set" then "" else ": "}${pkgs.lib.makeBinPath deps}" else ""} \
              --unset LD_LIBRARY_PATH \
              ${pkgs.lib.concatStringsSep " " extraWrapArgs}
          done
          for f in $(grep -rl '@BIN@' $out); do
            substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
          done
        '';

        # Helper: build a moxin that has bun+zx compiled scripts in src/.
        mkBunMoxin = name: deps: extraEntrypoints: { pathMode ? "set" }: let
          bunBinaries = bunLib.buildBunBinaries {
            pname = "${name}-moxin-scripts";
            version = "0.1.0";
            src = pkgs.lib.fileset.toSource {
              root = ./.;
              fileset = with pkgs.lib.fileset; unions [
                ./moxins/${name}/src
                ./package.json
                ./bun.lock
              ];
            };
            bunNix = ./bun.nix;
            entrypoints = extraEntrypoints;
            runtimeInputs = deps;
          };
        in pkgs.runCommand "${name}-moxin" {
          nativeBuildInputs = [ pkgs.makeWrapper ];
        } ''
          cp -r ${./moxins/${name}} $out
          chmod -R u+w $out
          rm -rf $out/src
          mkdir -p $out/bin
          for f in $out/bin/*; do [ -e "$f" ] && chmod +x "$f"; done
          # Link bun-compiled binaries into bin/.
          for f in ${bunBinaries}/bin/*; do
            ln -sf "$f" "$out/bin/$(basename "$f")"
          done
          for f in $out/bin/*; do
            [ -L "$f" ] && continue
            wrapProgram "$f" \
              ${if pathMode != "inherit" then "--${pathMode} PATH ${if pathMode == "set" then "" else ": "}${pkgs.lib.makeBinPath deps}" else ""} \
              --unset LD_LIBRARY_PATH
          done
          for f in $(grep -rl '@BIN@' $out); do
            substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
          done
        '';

        # Per-moxin derivations — each moxin is self-contained with its deps.
        chix-moxin = mkBunMoxin "chix" [
          pkgs.bash pkgs.coreutils pkgs.findutils pkgs.gnugrep pkgs.jq pkgs.manix
        ] {
          "flake-show" = "moxins/chix/src/flake-show.ts";
          "store-ls" = "moxins/chix/src/store-ls.ts";
        } { pathMode = "inherit"; };
        conch-moxin = mkMoxin "conch" [ pkgs.bash ] {};
        env-moxin = mkMoxin "env" [ pkgs.bash pkgs.coreutils pkgs.which ] { pathMode = "suffix"; };
        folio-moxin = mkMoxin "folio" [ pkgs.bash pkgs.coreutils pkgs.file pkgs.findutils pkgs.gawk pkgs.gnugrep pkgs.gnutar pkgs.gzip pkgs.jq ] {};
        folio-external-moxin = mkMoxin "folio-external" [ pkgs.bash pkgs.coreutils pkgs.file pkgs.findutils pkgs.gawk pkgs.gnugrep pkgs.gnutar pkgs.gzip pkgs.jq ] {};
        freud-moxin = mkMoxin "freud" [ pkgs.python3 ] {};
        get-hubbed-moxin = mkBunMoxin "get-hubbed" [
          pkgs.bash pkgs.coreutils pkgs.git pkgs-master.gh pkgs.jq pkgs.util-linux
        ] {
          "issue-get" = "moxins/get-hubbed/src/issue-get.ts";
          "issue-list" = "moxins/get-hubbed/src/issue-list.ts";
          "content-compare" = "moxins/get-hubbed/src/content-compare.ts";
          "content-search" = "moxins/get-hubbed/src/content-search.ts";
        } {};
        get-hubbed-external-moxin = mkBunMoxin "get-hubbed-external" [
          pkgs.bash pkgs.coreutils pkgs.git pkgs-master.gh pkgs.jq pkgs.util-linux
        ] {
          "issue-get" = "moxins/get-hubbed-external/src/issue-get.ts";
          "issue-list" = "moxins/get-hubbed-external/src/issue-list.ts";
        } {};
        grit-moxin = mkMoxin "grit" [ ] { pathMode = "inherit"; };
        hamster-moxin = mkBunMoxin "hamster" [
          pkgs.bash pkgs.coreutils pkgs.findutils pkgs.gawk pkgs.gnused pkgs-master.go_1_26
        ] {
          "doc" = "moxins/hamster/src/doc.ts";
          "src" = "moxins/hamster/src/src.ts";
          "mod-read" = "moxins/hamster/src/mod-read.ts";
        } { pathMode = "inherit"; };
        jira-moxin = mkMoxin "jira" [ pkgs.bash pkgs.jq pkgs-master-unfree.acli ] {};
        sisyphus-python = pkgs.python3.withPackages (ps: [ ps.atlassian-python-api ]);
        sisyphus-moxin = mkMoxin "sisyphus" [ sisyphus-python pkgs.bash pkgs.jq ] {};
        jq-moxin = mkMoxin "jq" [ pkgs.bash pkgs.jq ] {};
        just-us-agents-moxin = let
          listRecipes = bunLib.buildZxScript {
            pname = "just-list-recipes";
            version = "0.1.0";
            src = ./moxins/just-us-agents/src;
            entrypoint = "list-recipes.ts";
            runtimeInputs = [];
          };
        in pkgs.runCommand "just-us-agents-moxin" {
          nativeBuildInputs = [ pkgs.makeWrapper ];
        } ''
          cp -r ${./moxins/just-us-agents} $out
          chmod -R u+w $out
          rm -rf $out/src
          mkdir -p $out/bin
          for f in $out/bin/*; do [ -e "$f" ] && chmod +x "$f"; done
          ln -sf ${listRecipes}/bin/just-list-recipes $out/bin/list-recipes
          for f in $out/bin/*; do
            [ -L "$f" ] && continue
            wrapProgram "$f" --unset LD_LIBRARY_PATH
          done
          for f in $(grep -rl '@BIN@' $out); do
            substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
          done
        '';
        man-moxin = mkMoxin "man" [
          pkgs.bash pkgs.coreutils pkgs.gawk pkgs.gnugrep pkgs.gzip
          pkgs.man-db pkgs.mandoc pkgs.manix pkgs.pandoc
          maneater.packages.${system}.default
        ] {
          extraWrapArgs = [
            # Prefix moxy's own man pages while preserving the caller's MANPATH.
            "--prefix" "MANPATH" ":" "${moxy-man}/share/man"
          ];
          pathMode = "suffix";
        };
        rg-moxin = mkMoxin "rg" [ pkgs.bash pkgs-master.ripgrep ] {};

        # Symlink-only aggregation of all per-moxin derivations.
        moxy-moxins = pkgs.runCommand "moxy-moxins" {} ''
          mkdir -p $out/share/moxy/moxins
          ln -s ${chix-moxin} $out/share/moxy/moxins/chix
          ln -s ${conch-moxin} $out/share/moxy/moxins/conch
          ln -s ${env-moxin} $out/share/moxy/moxins/env
          ln -s ${folio-moxin} $out/share/moxy/moxins/folio
          ln -s ${folio-external-moxin} $out/share/moxy/moxins/folio-external
          ln -s ${freud-moxin} $out/share/moxy/moxins/freud
          ln -s ${get-hubbed-moxin} $out/share/moxy/moxins/get-hubbed
          ln -s ${get-hubbed-external-moxin} $out/share/moxy/moxins/get-hubbed-external
          ln -s ${grit-moxin} $out/share/moxy/moxins/grit
          ln -s ${hamster-moxin} $out/share/moxy/moxins/hamster
          ln -s ${jira-moxin} $out/share/moxy/moxins/jira
          ln -s ${sisyphus-moxin} $out/share/moxy/moxins/sisyphus
          ln -s ${jq-moxin} $out/share/moxy/moxins/jq
          ln -s ${just-us-agents-moxin} $out/share/moxy/moxins/just-us-agents
          ln -s ${man-moxin} $out/share/moxy/moxins/man
          ln -s ${rg-moxin} $out/share/moxy/moxins/rg
        '';

        moxy = pkgs.buildGoApplication {
          pname = "moxy";
          version = "0.1.0";
          src = moxySrc;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          nativeBuildInputs = [ pkgs.makeWrapper ];
          ldflags = [
            "-X"
            "github.com/amarbel-llc/moxy/internal/native.defaultSystemMoxinDir=${moxy-moxins}/share/moxy/moxins"
          ];
          postInstall = ''
            $out/bin/moxy generate-plugin $out
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
          paths = [
            moxy
            moxy-moxins
          ];
        };

        # Static Go binary for non-nix distribution (no wrapProgram, no
        # ldflags). Moxin discovery uses exe-relative path resolution.
        moxy-static = pkgs.buildGoApplication {
          pname = "moxy";
          version = "0.1.0";
          src = moxySrc;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          CGO_ENABLED = 0;
        };

        # Unwrapped moxin helper: replaces @BIN@ with relative "bin"
        # path (resolved at runtime by moxy against the moxin SourceDir).
        # Scripts rely on ambient PATH (provided by Homebrew depends_on).
        mkBrewMoxin = name: pkgs.runCommand "${name}-brew-moxin" {} ''
          cp -r ${./moxins/${name}} $out
          chmod -R u+w $out
          rm -rf $out/src
          chmod +x $out/bin/* 2>/dev/null || true
          for f in $out/*.toml; do
            [ "$(basename "$f")" = "_moxin.toml" ] && continue
            ${pkgs.gnused}/bin/sed -i 's|@BIN@|bin|g' "$f"
          done
        '';

        # Unwrapped bun moxin helper: builds JS bundles from TypeScript
        # sources and creates portable wrapper scripts that call bun from
        # PATH (no nix store references). entrypoints is an attrset of
        # name → source path relative to the flake root.
        mkBrewBunMoxin = name: entrypoints: let
          # Reuse buildBunBinaries to get the nix-wrapped output, then
          # extract the bundle path from the wrapper scripts.
          bunBins = bunLib.buildBunBinaries {
            pname = "${name}-brew-moxin-scripts";
            version = "0.1.0";
            src = pkgs.lib.fileset.toSource {
              root = ./.;
              fileset = with pkgs.lib.fileset; unions [
                ./moxins/${name}/src
                ./package.json
                ./bun.lock
              ];
            };
            bunNix = ./bun.nix;
            entrypoints = entrypoints;
            runtimeInputs = [];
          };
        in pkgs.runCommand "${name}-brew-moxin" {} ''
          cp -r ${./moxins/${name}} $out
          chmod -R u+w $out
          rm -rf $out/src
          mkdir -p $out/bin $out/lib

          # Extract the bundle dir from one of the wrapper scripts.
          # Wrappers look like: exec /nix/store/.../bin/bun /nix/store/.../<name>.js "$@"
          bundle_dir=""
          for f in ${bunBins}/bin/*; do
            js_path=$(grep -oE '/nix/store/[^ ]+\.js' "$f" | head -1)
            if [[ -n "$js_path" ]]; then
              bundle_dir=$(dirname "$js_path")
              break
            fi
          done
          if [[ -z "$bundle_dir" ]]; then
            echo "ERROR: could not extract bundle dir from wrapper scripts" >&2
            exit 1
          fi

          # Copy all bundled JS files into lib/.
          cp "$bundle_dir"/*.js $out/lib/

          # For each bun entrypoint, create a portable wrapper.
          for f in ${bunBins}/bin/*; do
            binname=$(basename "$f")
            js_path=$(grep -oE '/nix/store/[^ ]+\.js' "$f" | head -1)
            jsfile=$(basename "$js_path")
            cat > "$out/bin/$binname" <<WRAPPER
          #!/usr/bin/env bash
          exec bun "\$(dirname "\$0")/../lib/$jsfile" "\$@"
          WRAPPER
            chmod +x "$out/bin/$binname"
          done

          # Resolve @BIN@ to relative bin path (resolved at runtime via SourceDir).
          for f in $out/*.toml; do
            [ "$(basename "$f")" = "_moxin.toml" ] && continue
            ${pkgs.gnused}/bin/sed -i 's|@BIN@|bin|g' "$f"
          done
        '';

        # Moxins included in Homebrew distribution (those with deps
        # available in Homebrew).
        brew-moxins = {
          conch = mkBrewMoxin "conch";
          env = mkBrewMoxin "env";
          folio = mkBrewMoxin "folio";
          folio-external = mkBrewMoxin "folio-external";
          freud = mkBrewMoxin "freud";
          grit = mkBrewMoxin "grit";
          jq = mkBrewMoxin "jq";
          rg = mkBrewMoxin "rg";
          get-hubbed = mkBrewBunMoxin "get-hubbed" {
            "issue-get" = "moxins/get-hubbed/src/issue-get.ts";
            "issue-list" = "moxins/get-hubbed/src/issue-list.ts";
            "content-compare" = "moxins/get-hubbed/src/content-compare.ts";
            "content-search" = "moxins/get-hubbed/src/content-search.ts";
          };
          get-hubbed-external = mkBrewBunMoxin "get-hubbed-external" {
            "issue-get" = "moxins/get-hubbed-external/src/issue-get.ts";
            "issue-list" = "moxins/get-hubbed-external/src/issue-list.ts";
          };
          hamster = mkBrewBunMoxin "hamster" {
            "doc" = "moxins/hamster/src/doc.ts";
            "src" = "moxins/hamster/src/src.ts";
            "mod-read" = "moxins/hamster/src/mod-read.ts";
          };
          just-us-agents = mkBrewBunMoxin "just-us-agents" {
            "list-recipes" = "moxins/just-us-agents/src/list-recipes.ts";
          };
          man = mkBrewMoxin "man";
          sisyphus = mkBrewMoxin "sisyphus";
        };

        # Tarball for Homebrew distribution. Layout:
        #   bin/moxy
        #   share/moxy/moxins/{env,folio,...}/
        #   share/man/man{1,5,7}/
        brew-tarball = let
          arch = if pkgs.stdenv.hostPlatform.isAarch64 then "arm64"
                 else "amd64";
          os = if pkgs.stdenv.isDarwin then "darwin" else "linux";
        in pkgs.runCommand "moxy-brew-tarball" {} ''
          staging=$TMPDIR/moxy
          mkdir -p $staging/bin
          mkdir -p $staging/share/moxy/moxins
          mkdir -p $staging/share/man/man1
          mkdir -p $staging/share/man/man5
          mkdir -p $staging/share/man/man7

          # Static Go binary
          cp ${moxy-static}/bin/moxy $staging/bin/moxy

          # Unwrapped moxins — @BIN@ left as placeholder, resolved by the
          # Homebrew formula at install time via inreplace.
          ${pkgs.lib.concatStringsSep "\n" (pkgs.lib.mapAttrsToList (name: drv: ''
            cp -rL ${drv} $staging/share/moxy/moxins/${name}
            chmod -R u+w $staging/share/moxy/moxins/${name}
          '') brew-moxins)}

          # Man pages
          cp ${./cmd/moxy/moxy.1} $staging/share/man/man1/moxy.1
          cp ${./cmd/moxy/moxyfile.5} $staging/share/man/man5/moxyfile.5
          cp ${./cmd/moxy/moxy-hooks.5} $staging/share/man/man5/moxy-hooks.5
          cp ${./cmd/moxy/moxin.7} $staging/share/man/man7/moxin.7

          # Create tarball
          mkdir -p $out
          tar -czf $out/moxy-0.1.0-${os}-${arch}.tar.gz -C $TMPDIR moxy
        '';

        # Per-moxin tarballs for standalone install script. Each contains a
        # single moxin directory suitable for extraction to
        # ~/.local/share/moxy/moxins/<name>/
        mkStandaloneMoxinTarball = name: drv: let
          arch = if pkgs.stdenv.hostPlatform.isAarch64 then "arm64"
                 else "amd64";
          os = if pkgs.stdenv.isDarwin then "darwin" else "linux";
        in pkgs.runCommand "${name}-moxin-tarball" {} ''
          staging=$TMPDIR/${name}
          cp -rL ${drv} $staging
          chmod -R u+w $staging
          mkdir -p $out
          tar -czf $out/${name}-moxin-${os}-${arch}.tar.gz -C $TMPDIR ${name}
        '';

        standalone-moxin-tarballs = builtins.mapAttrs mkStandaloneMoxinTarball brew-moxins;
      in
      {
        packages = {
          inherit moxy moxy-moxins moxy-static brew-tarball standalone-moxin-tarballs;
          default = combined;
        };

        devShells.default = pkgs-master.mkShell {
          packages = [
            pkgs-master.go_1_26
            pkgs-master.delve
            pkgs-master.gofumpt
            pkgs-master.golangci-lint
            pkgs-master.golines
            pkgs-master.gopls
            pkgs-master.gotools
            pkgs-master.govulncheck
            gomod2nix.packages.${system}.default
            pkgs.just
            pkgs.manix
            pkgs.man-db
            pkgs.mandoc
            pkgs.pandoc
            pkgs.ripgrep
            pkgs.which # sandcastle's whichSync shells out to `which` (bob#98)
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
            bob.packages.${system}.batman
            bob.packages.${system}.grit
            bob.packages.${system}.lux
            purse-first.packages.${system}.purse-first
            tommy.packages.${system}.default
          ];
        };
      }
    ));
}
