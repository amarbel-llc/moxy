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

        pkgs-master-unfree = import nixpkgs-master {
          inherit system;
          config.allowUnfreePredicate = pkg: builtins.elem (pkgs.lib.getName pkg) [ "acli" "acli-unwrapped" ];
        };

        # Filtered sources so moxy and maneater rebuild independently.
        # maneaterSrc is explicit (rarely gains new internal/ deps).
        # moxySrc is everything else — new internal/ packages land here
        # automatically.
        commonGoFiles = with pkgs.lib.fileset; unions [
          ./go.mod
          ./go.sum
          ./gomod2nix.toml
        ];

        maneaterSrc = pkgs.lib.fileset.toSource {
          root = ./.;
          fileset = with pkgs.lib.fileset; unions [
            commonGoFiles
            ./cmd/maneater
            ./internal/embedding
          ];
        };

        moxySrc = pkgs.lib.fileset.toSource {
          root = ./.;
          fileset = with pkgs.lib.fileset; difference (unions [
            commonGoFiles
            ./cmd/moxy
            ./internal
            ./builtin-servers
            ./libexec
          ]) ./internal/embedding;
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
            cp ${./cmd/moxy/moxy-native.7} $out/share/man/man7/moxy-native.7

            # Install moxin configs
            mkdir -p $out/share/moxy/moxins
            cp ${./moxins}/*.toml $out/share/moxy/moxins/

            # Install freud scripts and wrap with python3 on PATH
            mkdir -p $out/libexec/moxy
            cp ${./libexec}/* $out/libexec/moxy/
            chmod +x $out/libexec/moxy/*
            for f in $out/libexec/moxy/freud-*; do
              wrapProgram "$f" --prefix PATH : ${pkgs.python3}/bin
            done
            for f in $out/libexec/moxy/just-*; do
              wrapProgram "$f" \
                --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.just pkgs.jq ]}
            done
            for f in $out/libexec/moxy/man-*; do
              wrapProgram "$f" \
                --prefix PATH : ${
                  pkgs.lib.makeBinPath [ pkgs.mandoc pkgs.pandoc maneater ]
                }
            done
            for f in $out/libexec/moxy/jira-*; do
              wrapProgram "$f" \
                --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs-master-unfree.acli ]}
            done

            # Rewrite __LIBEXEC__ placeholder to absolute nix store path
            sed -i "s|__LIBEXEC__|$out/libexec/moxy|g" $out/share/moxy/moxins/*.toml
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
          src = maneaterSrc;
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
              mkdir -p $out/bin $out/share/man/man1 $out/share/man/man5
              makeWrapper ${maneater-unwrapped}/bin/maneater $out/bin/maneater \
                --prefix PATH : ${
                  pkgs.lib.makeBinPath [
                    pkgs.mandoc
                    pkgs.pandoc
                    pkgs.tldr
                    pkgs-master.go_1_26
                  ]
                } \
                --set MANEATER_CONFIG ${maneater-models-toml}
              cp ${./cmd/maneater/maneater.1} $out/share/man/man1/maneater.1
              cp ${./cmd/maneater/maneater.toml.5} $out/share/man/man5/maneater.toml.5
            '';
        combined = pkgs.symlinkJoin {
          name = "moxy";
          paths = [
            moxy
            maneater
          ];
        };
      in
      {
        packages = {
          inherit
            moxy
            maneater
            ;
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
          # Explicit nix store man page paths for the bats test suite.
          # zz-tests_bats/common.bash re-exports this as MANPATH so that
          # maneater's locateSource() resolves to exactly these paths and
          # nothing else — no host man pages, no host $MANPATH ordering.
          MANEATER_TEST_MANPATH = "${pkgs.jq.man}/share/man:${pkgs.coreutils-full}/share/man";
        };
      }
    ));
}
