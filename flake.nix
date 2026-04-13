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

        # Helper: build a moxin with bin/ scripts wrapped with PATH deps.
        mkMoxin = name: deps: pkgs.runCommand "${name}-moxin" {
          nativeBuildInputs = [ pkgs.makeWrapper ];
        } ''
          cp -r ${./moxins/${name}} $out
          chmod -R u+w $out
          chmod +x $out/bin/*
          for f in $out/bin/*; do
            wrapProgram "$f" --set PATH ${pkgs.lib.makeBinPath deps}
          done
          for f in $(grep -rl '@BIN@' $out); do
            substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
          done
        '';

        # Helper: build a moxin that has bun+zx compiled scripts in src/.
        mkBunMoxin = name: deps: extraEntrypoints: let
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
          chmod +x $out/bin/*
          # Link bun-compiled binaries into bin/.
          for f in ${bunBinaries}/bin/*; do
            ln -sf "$f" "$out/bin/$(basename "$f")"
          done
          for f in $out/bin/*; do
            [ -L "$f" ] && continue
            wrapProgram "$f" --set PATH ${pkgs.lib.makeBinPath deps}
          done
          for f in $(grep -rl '@BIN@' $out); do
            substitute "$f" "$f" --replace-fail "@BIN@" "$out/bin"
          done
        '';

        # Per-moxin derivations — each moxin is self-contained with its deps.
        chix-moxin = mkBunMoxin "chix" [ pkgs.bash pkgs.nix pkgs.findutils ] {
          "flake-show" = "moxins/chix/src/flake-show.ts";
        };
        env-moxin = mkMoxin "env" [ pkgs.bash pkgs.coreutils ];
        folio-moxin = mkMoxin "folio" [ pkgs.bash pkgs.coreutils pkgs.findutils pkgs.gawk ];
        folio-external-moxin = mkMoxin "folio-external" [ pkgs.bash pkgs.coreutils pkgs.findutils pkgs.gawk ];
        freud-moxin = mkMoxin "freud" [ pkgs.python3 pkgs.jq ];
        get-hubbed-moxin = mkBunMoxin "get-hubbed" [ pkgs.bash pkgs-master.gh pkgs.jq ] {
          "issue-get" = "moxins/get-hubbed/src/issue-get.ts";
        };
        get-hubbed-external-moxin = mkMoxin "get-hubbed-external" [ pkgs.bash pkgs-master.gh pkgs.jq ];
        grit-moxin = mkMoxin "grit" [ pkgs.bash pkgs.git ];
        hamster-moxin = mkMoxin "hamster" [ pkgs.bash pkgs-master.go_1_26 pkgs.gnused pkgs.findutils ];
        jira-moxin = mkMoxin "jira" [ pkgs.bash pkgs-master-unfree.acli ];
        jq-moxin = mkMoxin "jq" [ pkgs.bash pkgs.jq ];
        just-us-agents-moxin = mkMoxin "just-us-agents" [ pkgs.bash pkgs.just pkgs.coreutils pkgs.findutils ];
        man-moxin = mkMoxin "man" [
          pkgs.bash pkgs.gzip pkgs.gnugrep pkgs.gawk pkgs.man-db pkgs.mandoc pkgs.pandoc pkgs.manix
          maneater.packages.${system}.default
        ];
        rg-moxin = mkMoxin "rg" [ pkgs.bash pkgs-master.ripgrep ];

        # Symlink-only aggregation of all per-moxin derivations.
        moxy-moxins = pkgs.runCommand "moxy-moxins" {} ''
          mkdir -p $out/share/moxy/moxins
          ln -s ${chix-moxin} $out/share/moxy/moxins/chix
          ln -s ${env-moxin} $out/share/moxy/moxins/env
          ln -s ${folio-moxin} $out/share/moxy/moxins/folio
          ln -s ${folio-external-moxin} $out/share/moxy/moxins/folio-external
          ln -s ${freud-moxin} $out/share/moxy/moxins/freud
          ln -s ${get-hubbed-moxin} $out/share/moxy/moxins/get-hubbed
          ln -s ${get-hubbed-external-moxin} $out/share/moxy/moxins/get-hubbed-external
          ln -s ${grit-moxin} $out/share/moxy/moxins/grit
          ln -s ${hamster-moxin} $out/share/moxy/moxins/hamster
          ln -s ${jira-moxin} $out/share/moxy/moxins/jira
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
            mkdir -p $out/share/man/man1 $out/share/man/man5 $out/share/man/man7
            cp ${./cmd/moxy/moxy.1} $out/share/man/man1/moxy.1
            cp ${./cmd/moxy/moxyfile.5} $out/share/man/man5/moxyfile.5
            cp ${./cmd/moxy/moxy-hooks.5} $out/share/man/man5/moxy-hooks.5
            cp ${./cmd/moxy/moxin.7} $out/share/man/man7/moxin.7

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
      in
      {
        packages = {
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
        };
      }
    ));
}
