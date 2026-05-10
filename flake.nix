{
  inputs = {
    nixpkgs.url = "github:amarbel-llc/nixpkgs";
    nixpkgs-master.url = "github:NixOS/nixpkgs/e2dde111aea2c0699531dc616112a96cd55ab8b5";
    # Pinned to the last upstream nixpkgs commit where pkgs.gomarkdoc still
    # builds. A regression after 2026-03-23 (still present on master as of
    # 2026-05-04) breaks gomarkdoc's checkPhase — used only as the source of
    # `pkgs.gomarkdoc` for hamster-moxin's @GOMARKDOC@ substitution. Remove
    # this input once NixOS/nixpkgs#516481 lands.
    nixpkgs-gomarkdoc-pin.url = "github:NixOS/nixpkgs/4590696c8693fea477850fe379a01544293ca4e2";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

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

    # amarbel-llc/bats provides bats-libs (the bundled library tree
    # consumed by mkBatsLane's batsLibPath). It used to also provide
    # batman (the sandcastle-wrapping bats wrapper) for the devshell,
    # but #249 moved bats execution into the nix sandbox — the devshell
    # uses pkgs.bats directly now. Used to come via amarbel-llc/bob,
    # but bob was dropped — moxy doesn't depend on anything else it
    # shipped.
    bats = {
      url = "github:amarbel-llc/bats";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.utils.follows = "utils";
    };

    bun = {
      url = "github:amarbel-llc/bun";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    # madder is the content-addressable blob store backing the moxin
    # result cache. Pinned at build time so `moxy version` reports an
    # auditable revision; users can override with
    # `nix build .#moxy --override-input madder github:amarbel-llc/madder/<rev>`.
    madder = {
      url = "github:amarbel-llc/madder";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-master,
      nixpkgs-gomarkdoc-pin,
      utils,
      purse-first,
      tommy,
      maneater,
      bats,
      bun,
      madder,
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
        pkgs = import nixpkgs { inherit system; };

        pkgs-master = import nixpkgs-master {
          inherit system;
        };

        # See nixpkgs-gomarkdoc-pin in inputs above.
        pkgs-gomarkdoc-pin = import nixpkgs-gomarkdoc-pin {
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

        # version.env at repo root is the single source of truth for
        # the release version. Burnt into the binary via the fork's
        # auto-injected -ldflags. `just bump-version` sed-rewrites
        # version.env. Match expression captures everything after
        # `MOXY_VERSION=` up to the line break.
        moxyVersion = builtins.head (builtins.match
          ".*MOXY_VERSION=([^\n]+).*"
          (builtins.readFile ./version.env));
        # shortRev for clean builds, dirtyShortRev for dirty working
        # trees (so devshell builds show `dirty-abcdef` rather than
        # masquerading as a clean release), "unknown" as a last-resort
        # fallback.
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
          cp ${./cmd/moxy/moxy-restart.7} $out/share/man/man7/moxy-restart.7
          MANPATH=$out/share/man mandb --no-purge --create $out/share/man
        '';

        # Helper: build a moxin with bin/ scripts wrapped with PATH deps.
        # extraSubstitutions: attrset of {NAME = "/abs/path";} pairs. Each
        # `@NAME@` placeholder anywhere under the moxin tree is replaced with
        # the literal value via `substitute`, so resolved store paths are
        # baked into the scripts at build time — same convention as `@BIN@`
        # and as mkBunMoxin's extraSubstitutions.
        mkMoxin = name: deps: { pathMode ? "set", extraWrapArgs ? [], extraSubstitutions ? {} }: pkgs.runCommand "${name}-moxin" {
          nativeBuildInputs = [ pkgs.makeWrapper ] ++ deps;
        } (''
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
        '' + pkgs.lib.concatMapStringsSep "\n" (placeholder: ''
          for f in $(grep -rl "@${placeholder}@" $out 2>/dev/null || true); do
            substitute "$f" "$f" --replace-fail "@${placeholder}@" "${extraSubstitutions.${placeholder}}"
          done
        '') (builtins.attrNames extraSubstitutions));

        # Helper: build a moxin that has bun+zx compiled scripts in src/.
        # extraSubstitutions: attrset of {NAME = "/abs/path";} pairs.
        # Before bundling, each `@NAME@` placeholder in the moxin's TS source
        # is replaced with the literal value via `substitute`, so the bundler
        # bakes the resolved store path directly into the JS — no runtime
        # env-var or PATH indirection. Same convention as `@BIN@` elsewhere.
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
          nativeBuildInputs = [ pkgs.makeWrapper ] ++ deps;
        } ''
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
              ${if pathMode != "inherit" then "--${pathMode} PATH ${if pathMode == "set" then "" else ": "}${pkgs.lib.makeBinPath deps}" else ""} \
              --unset LD_LIBRARY_PATH \
              ${pkgs.lib.concatStringsSep " " extraWrapArgs}
          done
          for f in $(grep -rl '@BIN@' $out); do
            substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
          done
        '';

        # Per-moxin derivations — each moxin is self-contained with its deps.
        # @WASM_DIR@ resolves at build time to the moxin's vendored wasm dir
        # (the source `moxins/arboretum/wasm` becomes a deterministic store
        # path). Same convention as hamster's @GOMARKDOC@/@PANDOC@: pre-bundle
        # substitution bakes the absolute store path into outline.js so the
        # bundled JS can locate both web-tree-sitter's runtime wasm and the
        # language grammars without any PATH or env-var indirection.
        arboretum-moxin = mkBunMoxin "arboretum" [
          pkgs.bash pkgs.ast-grep pkgs.pandoc
        ] {
          "outline" = "moxins/arboretum/src/outline.ts";
          "search" = "moxins/arboretum/src/search.ts";
          "rewrite" = "moxins/arboretum/src/rewrite.ts";
          # md-* tools shell out to pandoc for markdown AST work. Same gfm
          # reader the (now-retired) pandoc moxin used.
          "md-toc" = "moxins/arboretum/src/md-toc.ts";
          "md-section" = "moxins/arboretum/src/md-section.ts";
          "md-anchor" = "moxins/arboretum/src/md-anchor.ts";
        } {
          extraSubstitutions = {
            WASM_DIR = "${./moxins/arboretum/wasm}";
          };
        };
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
        } { pathMode = "suffix"; };
        conch-moxin = mkMoxin "conch" [ pkgs.bash ] {};
        env-moxin = mkMoxin "env" [ pkgs.bash pkgs.coreutils pkgs.which ] { pathMode = "suffix"; };
        folio-moxin = mkMoxin "folio" [ pkgs.bash pkgs.coreutils pkgs.file pkgs.findutils pkgs.gawk pkgs.gnugrep pkgs.gnutar pkgs.gzip pkgs.jq pkgs.tree ] {};
        freud-moxin = mkMoxin "freud" [ pkgs.python3 ] {};
        # pathMode = "suffix" so user PATH wins (and can shadow gh with a
        # stub in tests).
        get-hubbed-moxin = mkBunMoxin "get-hubbed" [
          pkgs.bash pkgs.coreutils pkgs.gawk pkgs.git pkgs-master.gh pkgs.jq pkgs.util-linux
        ] {
          "issue-get" = "moxins/get-hubbed/src/issue-get.ts";
          "issue-list" = "moxins/get-hubbed/src/issue-list.ts";
          "content-compare" = "moxins/get-hubbed/src/content-compare.ts";
          "content-search" = "moxins/get-hubbed/src/content-search.ts";
        } { pathMode = "suffix"; };
        grit-moxin = mkBunMoxin "grit" [
          pkgs.bash pkgs.coreutils pkgs.git
        ] {
          "push-stack" = "moxins/grit/src/push-stack.ts";
        } { pathMode = "inherit"; };
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

        # Symlink-only aggregation of all per-moxin derivations.
        moxy-moxins = pkgs.runCommand "moxy-moxins" {} ''
          mkdir -p $out/share/moxy/moxins
          ln -s ${arboretum-moxin} $out/share/moxy/moxins/arboretum
          ln -s ${chix-moxin} $out/share/moxy/moxins/chix
          ln -s ${conch-moxin} $out/share/moxy/moxins/conch
          ln -s ${env-moxin} $out/share/moxy/moxins/env
          ln -s ${folio-moxin} $out/share/moxy/moxins/folio
          ln -s ${freud-moxin} $out/share/moxy/moxins/freud
          ln -s ${get-hubbed-moxin} $out/share/moxy/moxins/get-hubbed
          ln -s ${grit-moxin} $out/share/moxy/moxins/grit
          ln -s ${hamster-moxin} $out/share/moxy/moxins/hamster
          ln -s ${sisyphus-moxin} $out/share/moxy/moxins/sisyphus
          ln -s ${jq-moxin} $out/share/moxy/moxins/jq
          ln -s ${just-us-agents-moxin} $out/share/moxy/moxins/just-us-agents
          ln -s ${man-moxin} $out/share/moxy/moxins/man
          ln -s ${rg-moxin} $out/share/moxy/moxins/rg
          ln -s ${piers-moxin} $out/share/moxy/moxins/piers
          ln -s ${car-moxin} $out/share/moxy/moxins/car
          ln -s ${slip-moxin} $out/share/moxy/moxins/slip
          ln -s ${prison-moxin} $out/share/moxy/moxins/prison
          ln -s ${gmail-moxin} $out/share/moxy/moxins/gmail
          ln -s ${calendar-moxin} $out/share/moxy/moxins/calendar
          ln -s ${gws-moxin} $out/share/moxy/moxins/gws
        '';

        madder-bin = madder.packages.${system}.default;

        moxy = pkgs.buildGoApplication {
          pname = "moxy";
          version = moxyVersion;
          commit = moxyCommit;
          src = moxySrc;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          nativeBuildInputs = [ pkgs.makeWrapper pkgs.jq ];
          # The fork's buildGoApplication auto-injects
          # `-X main.version` and `-X main.commit` from the `version`
          # and `commit` attrs above. Only project-specific ldflags
          # need to live here.
          ldflags = [
            "-X" "github.com/amarbel-llc/moxy/internal/native.defaultSystemMoxinDir=${moxy-moxins}/share/moxy/moxins"
            "-X" "github.com/amarbel-llc/moxy/internal/native.defaultMadderBin=${madder-bin}/bin/madder"
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
          # pname is consulted by `pkgs.testers.batsLane` for lane derivation
          # naming; symlinkJoin doesn't set it by default, so spell it out.
          pname = "moxy";
          paths = [
            moxy
            moxy-moxins
          ];
        };

        # Bats integration test source tree, fed to `pkgs.testers.batsLane`
        # to run the suite inside the nix build sandbox. See #249 for the
        # batman/sandcastle interaction this replaces.
        batsSrc = pkgs.lib.fileset.toSource {
          root = ./zz-tests_bats;
          fileset = with pkgs.lib.fileset; unions [
            ./zz-tests_bats/common.bash
            ./zz-tests_bats/justfile
            ./zz-tests_bats/test-fixtures
            ./zz-tests_bats/test-permission-request-hook.mjs
            (fileFilter (f: f.hasExt "bats") ./zz-tests_bats)
          ];
        };

        # Helper for building a single bats lane against the combined
        # moxy + moxy-moxins symlinkJoin (so the binary's baked-in
        # defaultSystemMoxinDir resolves and madder/MOXIN_PATH wiring
        # is consistent with what real users see). Mirrors madder's
        # go/default.nix:40-54 pattern.
        mkBatsLane = { filter ? "!net_cap,!host_only", base ? combined }:
          pkgs.testers.batsLane {
            inherit base filter batsSrc;
            binaries = {
              MOXY_BIN   = { inherit base; name = "moxy"; };
              MADDER_BIN = { base = madder-bin; name = "madder"; };
            };
            batsLibPath = [ bats.packages.${system}.bats-libs.batsLibPath ];
            extraEnv = {
              BATS_TEST_TIMEOUT = "30";
              MOXIN_PATH        = "${moxy-moxins}/share/moxy/moxins";
              # grit_*.bats invoke wrapped scripts at $BIN by default
              # ($BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin),
              # which doesn't exist inside the nix sandbox. Tests fall
              # back to GRIT_BIN via ${GRIT_BIN:-$BIN}.
              GRIT_BIN  = "${grit-moxin}/bin";
              # freud_tool_usage.bats falls back to FREUD_BIN (wrapped
              # python script) when set; otherwise invokes python3
              # directly against the source tree (devshell path).
              FREUD_BIN = "${freud-moxin}/bin/tool-usage";
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
              pkgs.gzip
              pkgs.jq
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
            files = builtins.attrNames
              (pkgs.lib.filterAttrs
                (n: t: t == "regular" && pkgs.lib.hasSuffix ".bats" n)
                (builtins.readDir dir));
            extract = name:
              let
                m = builtins.match
                  ".*# bats file_tags=([a-zA-Z0-9_,.-]+).*"
                  (builtins.readFile (dir + "/${name}"));
              in
                if m == null then [ ]
                else pkgs.lib.splitString ","
                  (builtins.head m);
          in
            pkgs.lib.unique (pkgs.lib.flatten (map extract files));

        batsLaneOutputs =
          pkgs.lib.listToAttrs (map
            (t: pkgs.lib.nameValuePair "bats-${t}"
              (mkBatsLane { filter = t; }))
            batsTags) // {
            bats-default   = mkBatsLane { };
            bats-net_cap   = mkBatsLane { filter = "net_cap"; };
            bats-host_only = mkBatsLane { filter = "host_only"; };
          };

      in
      {
        packages = batsLaneOutputs // {
          inherit moxy moxy-moxins;
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
            pkgs.gomod2nix
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
            # to pkgs.testers.batsLane (`just test-bats`). Devshell needs
            # only the raw bats binary now, for `just test-bats-dev`.
            pkgs.bats
            purse-first.packages.${system}.purse-first
            tommy.packages.${system}.default
          ];
        };
      }
    ));
}
