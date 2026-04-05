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

        nomic-model = pkgs.fetchurl {
          url = "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf";
          hash = "sha256-PiQ0IWSz2UmRupaS/cDdCOP9c2Lgqsw5appcVKVEw7c=";
        };

        snowflake-model = pkgs.fetchurl {
          url = "https://huggingface.co/Casual-Autopsy/snowflake-arctic-embed-l-v2.0-gguf/resolve/main/snowflake-arctic-embed-l-v2.0-q8_0.gguf";
          hash = "sha256-C+gyDssPtuIF8KFBnOPUaINLxE0Cy/2l/RcbNoGxJZc=";
        };

        maneater-unwrapped = pkgs.buildGoApplication {
          pname = "maneater";
          version = "0.4.0";
          src = ./.;
          subPackages = [ "cmd/maneater" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
          CGO_ENABLED = "1";
          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [ pkgs.llama-cpp ];
        };

        maneater-models-toml = pkgs.writeText "models.toml" ''
          default = "snowflake"

          [models.nomic]
          path = "${nomic-model}"
          query-prefix = "search_query: "
          document-prefix = "search_document: "

          [models.snowflake]
          path = "${snowflake-model}"
          query-prefix = "query: "
          document-prefix = ""
        '';

        maneater =
          pkgs.runCommand "maneater-wrapped"
            {
              nativeBuildInputs = [ pkgs.makeWrapper ];
            }
            ''
              mkdir -p $out/bin $out/share/man/man1
              makeWrapper ${maneater-unwrapped}/bin/maneater $out/bin/maneater \
                --prefix PATH : ${
                  pkgs.lib.makeBinPath [
                    pkgs.mandoc
                    pkgs.pandoc
                    pkgs.tldr
                  ]
                } \
                --set MANEATER_CONFIG ${maneater-models-toml}
              cp ${./cmd/maneater/maneater.1} $out/share/man/man1/maneater.1
            '';
        folio-unwrapped = pkgs.buildGoApplication {
          pname = "folio";
          version = "0.1.0";
          src = ./.;
          subPackages = [ "cmd/folio" ];
          modules = ./gomod2nix.toml;
          go = pkgs-master.go_1_26;
          GOTOOLCHAIN = "local";
        };

        folio =
          pkgs.runCommand "folio-wrapped"
            {
              nativeBuildInputs = [ pkgs.makeWrapper ];
            }
            ''
              mkdir -p $out/bin
              makeWrapper ${folio-unwrapped}/bin/folio $out/bin/folio \
                --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.ripgrep ]}
            '';

        combined = pkgs.symlinkJoin {
          name = "moxy";
          paths = [
            moxy
            maneater
            folio
          ];
        };
      in
      {
        packages = {
          inherit moxy maneater folio;
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
            pkgs.mandoc
            pkgs.pandoc
            pkgs.pkg-config
            pkgs.ripgrep
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
