# moxy's conformist config, merged with conformist.lib.presets.eng in flake.nix
# (conformist.lib.evalModule). The eng preset enables the language-agnostic
# eng-convention linters (eng-versioning, flake-*, justfile-*); this file picks
# moxy's formatters, its two custom check-script linters (dead-jq, mypy), and the
# repo-specific excludes. It replaces the former treefmt.nix — the formatter set
# below mirrors it. See conformist(7), conformist-nix(7).
#
# This module is the single config source: flake.nix's `nix fmt` (build.wrapper),
# the read-only checks.formatting gate (build.check), and the store-pinned
# conformist-pre-commit / conformist-repair git hooks all eval it (each hook bakes
# its own /nix/store config), so there is no committed conformist.toml to keep in
# sync. tommy (the TOML formatter) and the dead-jq / mypy check scripts are
# store-path deps built in flake.nix and injected here via _module.args.
{
  pkgs,
  lib,
  tommy,
  deadJqChecker,
  pyTypesChecker,
  ...
}:
{
  # Go: goimports before gofumpt so the import-grouped output is re-canonicalized
  # by gofumpt (the same priority chain the old treefmt.nix used).
  programs.goimports.enable = true;
  programs.goimports.priority = 1;
  programs.gofumpt.enable = true;
  programs.gofumpt.priority = 2;

  # Nix (the flake + these modules themselves).
  programs.nixfmt.enable = true;

  # TypeScript / ESM moxin sources (bun-compiled tools). Scope to .ts/.mjs/.cjs.
  programs.prettier.enable = true;
  programs.prettier.includes = [
    "*.ts"
    "*.mjs"
    "*.cjs"
  ];

  # Python: first-party moxin scripts (sisyphus lib + sisyphus/freud bins).
  # ruff-format formats; ruff-check lints (per-file E402 ignores live in
  # ruff.toml). api-perms is a bash script (excluded by shebang) — keep it out.
  programs.ruff-format.enable = true;
  programs.ruff-format.includes = [
    "moxins/sisyphus/lib/*.py"
    "moxins/sisyphus/bin/*"
    "moxins/freud/bin/*"
  ];
  programs.ruff-format.excludes = [ "moxins/sisyphus/bin/api-perms" ];
  linters.ruff-check.enable = true;
  linters.ruff-check.includes = [
    "moxins/sisyphus/lib/*.py"
    "moxins/sisyphus/bin/*"
    "moxins/freud/bin/*"
  ];
  linters.ruff-check.excludes = [ "moxins/sisyphus/bin/api-perms" ];
  # ruff writes a .ruff_cache under the tree root; conformist's build.check runs
  # ruff against the read-only /nix/store source copy, so the cache write fails
  # with EACCES and aborts the lint. A one-shot check needs no cache — disable
  # it. (mkAfter appends after the module's base `check` arg.)
  settings.linter.ruff-check.options = lib.mkAfter [ "--no-cache" ];

  # shfmt: a raw stanza rather than programs.shfmt — the module cannot emit `-ci`
  # (case-branch indent) and its default includes lack *.bats, both of which
  # moxy's shell/bats style requires. Spell out the command to match the old
  # treefmt.nix exactly: 2-space indent, simplify, case-indent.
  settings.formatter.shfmt = {
    command = "${pkgs.shfmt}/bin/shfmt";
    options = [
      "-w"
      "-i"
      "2"
      "-s"
      "-ci"
    ];
    includes = [
      "*.sh"
      "*.bash"
      "*.bats"
    ];
  };

  # tommy: CST-preserving TOML formatter (no conformist program module). Use the
  # explicit "${tommy}/bin/tommy" — a bare derivation in this freeform string
  # field would serialize to the store DIRECTORY, not the binary.
  settings.formatter.tommy = {
    command = "${tommy}/bin/tommy";
    options = [ "fmt" ];
    includes = [ "*.toml" ];
  };

  # Custom linters ported from the former hand-merged conformistConfig. Each
  # checker is a conformist.lib.writeCheckScript wrapper (sandbox-safe, deps
  # pinned) built in flake.nix and injected via _module.args.
  #
  # dead-jq: flag dead `jq -e` assertions in bats test bodies.
  settings.linter.dead-jq = {
    command = "${deadJqChecker}/bin/lint-dead-jq";
    includes = [ "zz-tests_bats/*.bats" ];
  };
  # mypy: lenient type-check (#10) of first-party moxin Python (reads ./mypy.ini).
  settings.linter.mypy = {
    command = "${pyTypesChecker}/bin/lint-py-types";
    includes = [
      "moxins/sisyphus/lib/*.py"
      "moxins/sisyphus/bin/*"
      "moxins/freud/bin/*"
    ];
  };

  # eng-versioning(7) derives the version key from go.mod's module path; moxy's
  # module leaf already yields MOXY_VERSION (matching version.env), but pin it
  # explicitly (as posh/madder do) to be robust to the derivation.
  linters.eng-versioning.key = "MOXY_VERSION";

  # Excludes layered on conformist's default-excludes. NB: an exclude here drops
  # the file before BOTH the formatters AND the whole-tree eng linters, so files
  # an enabled pure-eval linter keys on must NOT be excluded — in particular
  # version.env (eng-versioning) is deliberately kept, and no formatter matches
  # it anyway. The entries below are either generated files a formatter WOULD
  # otherwise rewrite (bun.nix→nixfmt, config_tommy.go→goimports,
  # gomod2nix.toml→tommy) or scratch/prose/vendored artifacts. (The sweatfile /
  # gomod2nix git-state linters live in the separate eng-impure eval, which does
  # not import this module, so excluding their files here does not blind them.)
  settings.excludes = [
    "flake.lock"
    "go.sum"
    "gomod2nix.toml"
    "bun.lock"
    "bun.nix"
    "internal/config/config_tommy.go"
    "zz-tests_bats/test-fixtures/**"
    "moxins/sisyphus/lib/_vendor/**"
    "sweatfile"
    "LICENSE"
    "*.md"
    "result"
    "result-*"
    ".tmp/**"
    ".direnv/**"
  ];
}
