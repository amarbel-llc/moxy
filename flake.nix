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

        gwsVersion = "0.22.5";
        gwsPlatform = {
          "aarch64-darwin" = { name = "aarch64-apple-darwin"; hash = "sha256-HSqf/VvJssLEtIYw2vCC+tE9nlfXQZiKLCSO7VYvfaw="; };
          "x86_64-darwin"  = { name = "x86_64-apple-darwin";  hash = "sha256-Ufm9cxQE1LuibDbi4w3WjFbczR+DTAElLLCxTWplRLI="; };
          "x86_64-linux"   = { name = "x86_64-unknown-linux-gnu";   hash = "sha256-3njs29LxqEzKAGOn7LxEAkD8FLbrzLsX9GRreSqMXB8="; };
          "aarch64-linux"  = { name = "aarch64-unknown-linux-gnu";  hash = "sha256-lEkCldlYDh6IV05xWgoWKZF0fRLWL4x7jcyCaLbBzqA="; };
        }.${system} or (throw "gws: unsupported system ${system}");

        gws-bin = pkgs.stdenv.mkDerivation {
          pname = "gws";
          version = gwsVersion;
          src = pkgs.fetchurl {
            url = "https://github.com/googleworkspace/cli/releases/download/v${gwsVersion}/google-workspace-cli-${gwsPlatform.name}.tar.gz";
            hash = gwsPlatform.hash;
          };
          sourceRoot = ".";
          installPhase = ''
            mkdir -p $out/bin
            cp gws $out/bin/gws
            chmod +x $out/bin/gws
          '';
        };

        moxyVersion = "0.6.3";
        moxyCommit = self.shortRev or self.dirtyShortRev or "unknown";

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
          ${if pathMode == "inherit" && extraWrapArgs == [] then ''
          # pathMode=inherit with no extra args: skip wrapProgram entirely so
          # scripts run with the host's unmodified environment.
          '' else ''
          for f in $out/bin/*; do
            wrapProgram "$f" \
              ${if pathMode != "inherit" then "--${pathMode} PATH ${if pathMode == "set" then "" else ": "}${pkgs.lib.makeBinPath deps}" else ""} \
              --unset LD_LIBRARY_PATH \
              ${pkgs.lib.concatStringsSep " " extraWrapArgs}
          done
          ''}
          for f in $(grep -rl '@BIN@' $out); do
            substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
          done
        '';

        # Helper: build a moxin that has bun+zx compiled scripts in src/.
        # extraSubstitutions: attrset of {NAME = "/abs/path";} pairs.
        # Before bundling, each `@NAME@` placeholder in the moxin's TS source
        # is replaced with the literal value via `substitute`, so the bundler
        # bakes the resolved store path directly into the JS — no runtime
        # env-var or PATH indirection. Same convention as `@BIN@` elsewhere.
        # Brew bundles (mkBrewBunMoxin) skip this step, leaving placeholders
        # intact; scripts must include a fallback (PATH lookup) for that case.
        mkBunMoxin = name: deps: extraEntrypoints: { pathMode ? "set", extraWrapArgs ? [], extraSubstitutions ? {} }: let
          rawSrc = pkgs.lib.fileset.toSource {
            root = ./.;
            fileset = with pkgs.lib.fileset; unions [
              ./moxins/${name}/src
              ./package.json
              ./bun.lock
            ];
          };
          src = if extraSubstitutions == {} then rawSrc else
            pkgs.runCommand "${name}-moxin-src" {} (''
              cp -rL ${rawSrc} $out
              chmod -R u+w $out
            '' + pkgs.lib.concatMapStringsSep "\n" (placeholder: ''
              for f in $(grep -rl "@${placeholder}@" $out 2>/dev/null || true); do
                substitute "$f" "$f" --replace-fail "@${placeholder}@" "${extraSubstitutions.${placeholder}}"
              done
            '') (builtins.attrNames extraSubstitutions));
          bunBinaries = bunLib.buildBunBinaries {
            pname = "${name}-moxin-scripts";
            version = moxyVersion;
            inherit src;
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
          #!/usr/bin/env bash
          exec $bun_bin $js_path "\$@"
          WRAPPER
            chmod +x "$out/bin/$binname"
          done
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

        # Per-moxin derivations — each moxin is self-contained with its deps.
        # chix uses pathMode = "suffix" so the user's nix binary wins (and
        # picks up the user's NIX_PATH / config), while manix + git + the
        # shell helpers are guaranteed to resolve from the wrapper's
        # PATH suffix when the ambient environment doesn't provide them.
        # Needed for chix.doc* (manix) and chix.flake-update / flake-lock
        # (nix shells out to git to stage the updated lock file).
        chix-moxin = mkBunMoxin "chix" [
          pkgs.bash pkgs.coreutils pkgs.findutils pkgs.git pkgs.gnugrep pkgs.jq pkgs.manix
        ] {
          "flake-check" = "moxins/chix/src/flake-check.ts";
          "flake-lock" = "moxins/chix/src/flake-lock.ts";
          "flake-show" = "moxins/chix/src/flake-show.ts";
          "flake-update" = "moxins/chix/src/flake-update.ts";
          "store-ls" = "moxins/chix/src/store-ls.ts";
          "try" = "moxins/chix/src/try.ts";
        } { pathMode = "suffix"; };
        conch-moxin = mkMoxin "conch" [ pkgs.bash ] {};
        env-moxin = mkMoxin "env" [ pkgs.bash pkgs.coreutils pkgs.which ] { pathMode = "suffix"; };
        folio-moxin = mkMoxin "folio" [ pkgs.bash pkgs.coreutils pkgs.file pkgs.findutils pkgs.gawk pkgs.gnugrep pkgs.gnutar pkgs.gzip pkgs.jq ] {};
        folio-external-moxin = mkMoxin "folio-external" [ pkgs.bash pkgs.coreutils pkgs.file pkgs.findutils pkgs.gawk pkgs.gnugrep pkgs.gnutar pkgs.gzip pkgs.jq ] {};
        freud-moxin = mkMoxin "freud" [ pkgs.python3 ] {};
        # pathMode = "suffix" so user PATH wins (and can shadow gh with a
        # stub in tests); watch-run / watch-remove use `awk` so gawk is
        # explicit in the wrapper deps.
        get-hubbed-moxin = mkBunMoxin "get-hubbed" [
          pkgs.bash pkgs.coreutils pkgs.gawk pkgs.git pkgs-master.gh pkgs.jq pkgs.util-linux
        ] {
          "issue-get" = "moxins/get-hubbed/src/issue-get.ts";
          "issue-list" = "moxins/get-hubbed/src/issue-list.ts";
          "content-compare" = "moxins/get-hubbed/src/content-compare.ts";
          "content-search" = "moxins/get-hubbed/src/content-search.ts";
        } { pathMode = "suffix"; };
        get-hubbed-external-moxin = mkBunMoxin "get-hubbed-external" [
          pkgs.bash pkgs.coreutils pkgs.git pkgs-master.gh pkgs.jq pkgs.util-linux
        ] {
          "issue-get" = "moxins/get-hubbed-external/src/issue-get.ts";
          "issue-list" = "moxins/get-hubbed-external/src/issue-list.ts";
        } {};
        grit-moxin = mkMoxin "grit" [ ] { pathMode = "inherit"; };
        # @GOMARKDOC@ / @PANDOC@ are baked into the bundled JS at build time
        # via mkBunMoxin's extraSubstitutions, so the markdown renderer path
        # doesn't depend on the user's PATH or any runtime env var. doc.ts
        # carries a PATH-fallback for non-nix builds (brew, devshell) where
        # the placeholders survive into the bundle unmodified.
        hamster-moxin = mkBunMoxin "hamster" [
          pkgs.bash pkgs.coreutils pkgs.findutils pkgs.gawk pkgs.gnused pkgs.jq pkgs-master.go_1_26
        ] {
          "doc" = "moxins/hamster/src/doc.ts";
          "doc-outline" = "moxins/hamster/src/doc-outline.ts";
          "src" = "moxins/hamster/src/src.ts";
          "mod-read" = "moxins/hamster/src/mod-read.ts";
        } {
          pathMode = "inherit";
          extraSubstitutions = {
            GOMARKDOC = "${pkgs.gomarkdoc}/bin/gomarkdoc";
            PANDOC = "${pkgs.pandoc}/bin/pandoc";
          };
        };
        sisyphus-python = pkgs.python3.withPackages (ps: [ ps.atlassian-python-api ]);
        sisyphus-moxin = mkMoxin "sisyphus" [ sisyphus-python pkgs.bash pkgs.jq ] {};
        jq-moxin = mkMoxin "jq" [ pkgs.bash pkgs.jq ] {};
        just-us-agents-moxin = let
          listRecipes = bunLib.buildZxScript {
            pname = "just-list-recipes";
            version = moxyVersion;
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
        pandoc-moxin = mkBunMoxin "pandoc" [ pkgs.bash pkgs.pandoc ] {
          "toc" = "moxins/pandoc/src/toc.ts";
          "section" = "moxins/pandoc/src/section.ts";
          "anchor" = "moxins/pandoc/src/anchor.ts";
        } {};

        gwsDeps = [ pkgs.bash pkgs.coreutils gws-bin ];
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
        } {};
        car-moxin = mkBunMoxin "car" gwsDeps {
          "search" = "moxins/car/src/search.ts";
          "get" = "moxins/car/src/get.ts";
          "list" = "moxins/car/src/list.ts";
          "export" = "moxins/car/src/export.ts";
          "doc-graph" = "moxins/car/src/doc-graph.ts";
        } {};
        slip-moxin = pkgs.runCommand "slip-moxin" {} ''
          cp -r ${./moxins/slip} $out
        '';
        prison-moxin = mkBunMoxin "prison" gwsDeps {
          "get" = "moxins/prison/src/get.ts";
        } {};
        gmail-moxin = mkBunMoxin "gmail" gwsDeps {
          "triage" = "moxins/gmail/src/triage.ts";
          "read" = "moxins/gmail/src/read.ts";
        } {};
        calendar-moxin = mkBunMoxin "calendar" gwsDeps {
          "agenda" = "moxins/calendar/src/agenda.ts";
        } {};
        gws-moxin = mkBunMoxin "gws" gwsDeps {
          "api" = "moxins/gws/src/api.ts";
        } {};

        walkie-talkie-moxin = mkMoxin "walkie-talkie" [
          pkgs.bash pkgs.coreutils pkgs.gnugrep
        ] {};

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
          ln -s ${sisyphus-moxin} $out/share/moxy/moxins/sisyphus
          ln -s ${jq-moxin} $out/share/moxy/moxins/jq
          ln -s ${just-us-agents-moxin} $out/share/moxy/moxins/just-us-agents
          ln -s ${man-moxin} $out/share/moxy/moxins/man
          ln -s ${rg-moxin} $out/share/moxy/moxins/rg
          ln -s ${pandoc-moxin} $out/share/moxy/moxins/pandoc
          ln -s ${piers-moxin} $out/share/moxy/moxins/piers
          ln -s ${car-moxin} $out/share/moxy/moxins/car
          ln -s ${slip-moxin} $out/share/moxy/moxins/slip
          ln -s ${prison-moxin} $out/share/moxy/moxins/prison
          ln -s ${gmail-moxin} $out/share/moxy/moxins/gmail
          ln -s ${calendar-moxin} $out/share/moxy/moxins/calendar
          ln -s ${gws-moxin} $out/share/moxy/moxins/gws
          ln -s ${walkie-talkie-moxin} $out/share/moxy/moxins/walkie-talkie
        '';

        moxy = pkgs.buildGoApplication {
          pname = "moxy";
          version = moxyVersion;
          src = moxySrc;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          nativeBuildInputs = [ pkgs.makeWrapper pkgs.jq ];
          ldflags = [
            "-X" "main.version=${moxyVersion}"
            "-X" "main.commit=${moxyCommit}"
            "-X" "github.com/amarbel-llc/moxy/internal/native.defaultSystemMoxinDir=${moxy-moxins}/share/moxy/moxins"
          ];
          postInstall = ''
            MOXY_MCP_BINARY="$out/bin/moxy" $out/bin/moxy generate-plugin $out

            # purse-first's generate-plugin doesn't emit a `version` field; inject
            # it from the nix-pinned moxyVersion so distributed plugin.json tracks
            # releases (Claude Code plugins spec requires semver `version`).
            pluginJson="$out/share/purse-first/moxy/.claude-plugin/plugin.json"
            jq --arg v "${moxyVersion}" '.version = $v' "$pluginJson" > "$pluginJson.tmp"
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

            # walkie-talkie plugin monitor + skill (see moxins/walkie-talkie).
            # Skill trips on-skill-invoke:walkie-talkie which starts the
            # monitor; monitor is the nix-wrapped script in the moxin itself.
            mkdir -p $out/share/purse-first/moxy/monitors
            substitute ${./monitors/monitors.json} $out/share/purse-first/moxy/monitors/monitors.json \
              --replace-fail "@WALKIE_TALKIE_MONITOR@" "${walkie-talkie-moxin}/bin/walkie-talkie-monitor" \
              --replace-fail "@GH_WATCH_MONITOR@" "${get-hubbed-moxin}/bin/gh-watch-monitor"
            mkdir -p $out/share/purse-first/moxy/skills/walkie-talkie
            cp ${./skills/walkie-talkie/SKILL.md} $out/share/purse-first/moxy/skills/walkie-talkie/SKILL.md
            mkdir -p $out/share/purse-first/moxy/skills/gh-watch
            cp ${./skills/gh-watch/SKILL.md} $out/share/purse-first/moxy/skills/gh-watch/SKILL.md

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
        # system moxin ldflags). Moxin discovery uses exe-relative path resolution.
        moxy-static = pkgs.buildGoApplication {
          pname = "moxy";
          version = moxyVersion;
          src = moxySrc;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          CGO_ENABLED = 0;
          ldflags = [ "-X" "main.version=${moxyVersion}" "-X" "main.commit=${moxyCommit}" ];
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
            version = moxyVersion;
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

          # Extract the bundle store path from one of the wrapper scripts.
          # bun >=9 may nest JS output in subdirectories, so we extract
          # the store path and use find instead of assuming flat layout.
          bundle_dir=""
          for f in ${bunBins}/bin/*; do
            bundle_dir=$(grep -oE '/nix/store/[^/]+' "$f" | grep bundle | head -1)
            [[ -n "$bundle_dir" ]] && break
          done
          if [[ -z "$bundle_dir" ]]; then
            echo "ERROR: could not extract bundle dir from wrapper scripts" >&2
            exit 1
          fi

          # For each bun entrypoint, find the JS bundle and copy to lib/.
          for f in ${bunBins}/bin/*; do
            binname=$(basename "$f")
            jsfile="$binname.js"
            if [[ -f "$bundle_dir/$jsfile" ]]; then
              js_path="$bundle_dir/$jsfile"
            else
              js_path=$(find "$bundle_dir" -name "$jsfile" -type f | head -1)
            fi
            if [[ -z "$js_path" ]]; then
              echo "ERROR: could not find $jsfile in $bundle_dir" >&2
              exit 1
            fi
            cp "$js_path" "$out/lib/$jsfile"
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
            "doc-outline" = "moxins/hamster/src/doc-outline.ts";
            "src" = "moxins/hamster/src/src.ts";
            "mod-read" = "moxins/hamster/src/mod-read.ts";
          };
          just-us-agents = mkBrewBunMoxin "just-us-agents" {
            "list-recipes" = "moxins/just-us-agents/src/list-recipes.ts";
          };
          man = mkBrewMoxin "man";
          pandoc = mkBrewBunMoxin "pandoc" {
            "toc" = "moxins/pandoc/src/toc.ts";
            "section" = "moxins/pandoc/src/section.ts";
            "anchor" = "moxins/pandoc/src/anchor.ts";
          };
          sisyphus = mkBrewMoxin "sisyphus";
          car = mkBrewBunMoxin "car" {
            "search" = "moxins/car/src/search.ts";
            "get" = "moxins/car/src/get.ts";
            "list" = "moxins/car/src/list.ts";
            "export" = "moxins/car/src/export.ts";
            "doc-graph" = "moxins/car/src/doc-graph.ts";
          };
          piers = mkBrewBunMoxin "piers" {
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
          };
          prison = mkBrewBunMoxin "prison" {
            "get" = "moxins/prison/src/get.ts";
          };
          gmail = mkBrewBunMoxin "gmail" {
            "triage" = "moxins/gmail/src/triage.ts";
            "read" = "moxins/gmail/src/read.ts";
          };
          calendar = mkBrewBunMoxin "calendar" {
            "agenda" = "moxins/calendar/src/agenda.ts";
          };
          gws = mkBrewBunMoxin "gws" {
            "api" = "moxins/gws/src/api.ts";
          };
          slip = mkBrewMoxin "slip";
        };

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

        # Full-release tarball consumed by package managers that expect one
        # archive with the binary, all moxins, and man pages (e.g. the
        # Homebrew formula). Layout:
        #   bin/moxy
        #   share/moxy/moxins/<name>/{_moxin.toml,<tool>.toml,bin/<tool>}
        #   share/man/man{1,5,7}/*
        #
        # Moxins come from brew-moxins (mkBrewMoxin / mkBrewBunMoxin). Their
        # TOMLs already have @BIN@ rewritten to the relative path "bin" at
        # build time; moxy joins that with the moxin's SourceDir at runtime,
        # so the installer doesn't need to inreplace anything. Moxin bin/
        # scripts rely on ambient PATH (bash, jq, git, gh, …), which the
        # formula provides via depends_on.
        #
        # The contract this derivation produces is pinned by the bats test
        # at zz-tests_bats/release_tarball.bats — modify with care, both
        # sides must change together.
        release-tarball = let
          arch = if pkgs.stdenv.hostPlatform.isAarch64 then "arm64"
                 else "amd64";
          os = if pkgs.stdenv.isDarwin then "darwin" else "linux";
        in pkgs.runCommand "moxy-release-tarball" {} ''
          staging=$TMPDIR/moxy
          mkdir -p $staging/bin
          mkdir -p $staging/share/moxy/moxins
          mkdir -p $staging/share/man/man1
          mkdir -p $staging/share/man/man5
          mkdir -p $staging/share/man/man7

          cp ${moxy-static}/bin/moxy $staging/bin/moxy

          ${pkgs.lib.concatStringsSep "\n" (pkgs.lib.mapAttrsToList (name: drv: ''
            cp -rL ${drv} $staging/share/moxy/moxins/${name}
            chmod -R u+w $staging/share/moxy/moxins/${name}
          '') brew-moxins)}

          cp ${./cmd/moxy/moxy.1} $staging/share/man/man1/moxy.1
          cp ${./cmd/moxy/moxyfile.5} $staging/share/man/man5/moxyfile.5
          cp ${./cmd/moxy/moxy-hooks.5} $staging/share/man/man5/moxy-hooks.5
          cp ${./cmd/moxy/moxin.7} $staging/share/man/man7/moxin.7

          mkdir -p $out
          tar -czf $out/moxy-${os}-${arch}.tar.gz -C $TMPDIR moxy
        '';
      in
      {
        packages = {
          inherit moxy moxy-moxins moxy-static release-tarball;
          default = combined;
        };

        # `standalone-moxin-tarballs` is an attrset of derivations (one per
        # moxin), not a single derivation. That's valid for `nix build
        # .#standalone-moxin-tarballs.<name>` — which resolves through
        # `legacyPackages` — but `flake check` rejects nested attrsets under
        # `packages`. See issue #161.
        legacyPackages = {
          inherit standalone-moxin-tarballs;
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
          # sandcastle needs macOS system binaries (sandbox-exec, which) that
          # live in /usr/bin. Nix devshells don't include /usr/bin on PATH by
          # default. See bob#98.
          shellHook = ''
            export PATH="$PATH:/usr/bin"
          '';
        };
      }
    ));
}
