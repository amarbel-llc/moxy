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

        moxy = pkgs.buildGoApplication {
          pname = "moxy";
          version = "0.1.0";
          src = moxySrc;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          nativeBuildInputs = [ pkgs.makeWrapper ];
          postInstall = ''
            $out/bin/moxy generate-plugin $out
            mkdir -p $out/share/man/man1 $out/share/man/man5 $out/share/man/man7
            cp ${./cmd/moxy/moxy.1} $out/share/man/man1/moxy.1
            cp ${./cmd/moxy/moxyfile.5} $out/share/man/man5/moxyfile.5
            cp ${./cmd/moxy/moxin.7} $out/share/man/man7/moxin.7

            # Wrap the moxy binary with inline moxin tool dependencies.
            # Inline scripts (folio, grit, rg, get-hubbed, chix, env, jq)
            # inherit PATH from the moxy process.
            wrapProgram $out/bin/moxy \
              --prefix PATH : ${
                pkgs.lib.makeBinPath [
                  pkgs.bash
                  pkgs.coreutils
                  pkgs.findutils
                  pkgs.gawk
                  pkgs.gnused
                  pkgs.gzip
                  pkgs.jq
                  pkgs.git
                  pkgs-master.gh
                  pkgs-master.ripgrep
                  pkgs.nix
                  pkgs.util-linux # column
                ]
              }
          '';
        };

        moxy-moxins = pkgs.runCommand "moxy-moxins" {
          nativeBuildInputs = [ pkgs.makeWrapper ];
        } ''
          mkdir -p $out/share/moxy/moxins
          cp -r ${./moxins}/*/ $out/share/moxy/moxins/
          chmod -R u+w $out/share/moxy/moxins

          mkdir -p $out/libexec/moxy
          cp ${./libexec}/* $out/libexec/moxy/
          chmod +x $out/libexec/moxy/*
          for f in $out/libexec/moxy/*; do
            wrapProgram "$f" \
              --set PATH ${
                pkgs.lib.makeBinPath [
                  pkgs.bash
                  pkgs.python3
                  pkgs.jq
                  pkgs.just
                  pkgs.mandoc
                  pkgs.pandoc
                  pkgs.manix
                  pkgs-master.go_1_26
                  pkgs-master-unfree.acli
                  maneater.packages.${system}.default
                ]
              }
          done

          for f in $(grep -rl '@LIBEXEC@' $out/share/moxy/moxins); do
            substitute "$f" "$f" --replace-fail "@LIBEXEC@" "$out/libexec/moxy"
          done
        '';

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
          inherit moxy;
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
