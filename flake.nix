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

        moxy = pkgs.buildGoApplication {
          pname = "moxy";
          version = "0.1.0";
          src = ./.;
          subPackages = [ "cmd/moxy" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          postInstall = ''
            $out/bin/moxy generate-plugin $out
          '';
        };

        manpage-unwrapped = pkgs.buildGoApplication {
          pname = "manpage";
          version = "0.2.0";
          src = ./.;
          subPackages = [ "cmd/manpage" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
        };

        manpage =
          pkgs.runCommand "manpage-wrapped"
            {
              nativeBuildInputs = [ pkgs.makeWrapper ];
            }
            ''
              mkdir -p $out/bin
              makeWrapper ${manpage-unwrapped}/bin/manpage $out/bin/manpage \
                --prefix PATH : ${
                  pkgs.lib.makeBinPath [
                    pkgs.mandoc
                    pkgs.pandoc
                  ]
                }
            '';
        combined = pkgs.symlinkJoin {
          name = "moxy";
          paths = [
            moxy
            manpage
          ];
        };
      in
      {
        packages = {
          inherit moxy manpage;
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
            pkgs.llama-cpp
            pkgs.pandoc
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
