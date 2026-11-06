---
date: 2026-11-06
status: proposal
scope: every open moxy issue on 2026-11-06 (188), plus a cleanup sweep
base: master 45ba2fc
---

# moxy issue grooming, 2026-11-06

This note groomed every open issue on linenisgreat/moxy at master 45ba2fc. It
also includes a code and docs cleanup sweep. The work was read-only: no issue
was closed, edited or relabelled. Everything under "Proposed forge actions" is
for the operator to decide.

**How the verdicts were made.** Four read-only agents each read a quarter of
the issues, with their comments, then checked the code and history. Each
verdict cites a commit or a `file:line`. I re-checked only a few by hand: the
`ReloadMoxin` and serve-moxin adapter findings, and the stale AGENTS.md folio
path. Treat the rest as evidence-backed leads, not settled facts. Anything
marked UNSURE was not settled. No build or test ran, so claims about runtime
behaviour, such as "#196 returns empty", come from a single live call or from
reading the code.

## Totals

| verdict | count |
|---|---:|
| READY (actionable now) | 59 |
| DESIGN (needs a decision) | 47 |
| STALE (subject gone or premise changed) | 42 |
| FIXED on master | 31 |
| BLOCKED on another repo | 5 |
| UNSURE | 3 |
| DUPLICATE | 1 |
| **total** | **188** |

New issue filed during grooming: #507 (serve-moxin V0 down-convert drops
cached resource blocks).

## Proposed forge actions (for the operator)

**Close as fixed:**

- #82, #129, #140, #162, #169, #178, #182, #186, #211, #224, #225, #247,
  #275, #323, #330, #375, #382, #408, #444, #485, #491, #492, #495, #497.
- #490: confirm with its lane, moxy/sunny-sumac.
- #493: or keep it open to revisit the dial defaults against live data.
- #23, and #1 (or retitle #1 to "quoting").

**Close as stale:**

- These are about moxins this repo no longer ships (hamster, sisyphus,
  get-hubbed, just-us-agents, conch, lux, brew, tarballs): #42, #51, #62, #68,
  #75, #78, #88, #93, #111, #115, #161, #174, #179, #188, #193, #199, #203,
  #204, #205, #206, #210, #213, #228, #232, #233, #235, #236, #241, #246,
  #257, #279, #287, #290, #332, #349, #354.
- These belong in another repo: #415 and #435 (smith), #428 (just-us),
  #483 (spinclass), #397 (smith umbrella).
- #266 is stale too: fold it into #476, or retitle it to the godyn cover lane.

**Close as duplicate:** #458 duplicates #467.

**Overlapping issues to merge or cross-link:**

- #302 and #434: both export mkMoxin as a `lib`.
- #96 and #263: both want a per-tool exec timeout.
- #90 and #139: freud overflow reporting.
- #266 and #476: the cover recipe.
- #278, #447 and #458 overlap #467: gws-family bun tool hygiene.
- #368 and #369: "session expired".
- #393 is superseded by #478.
- #320 reduces to #328 and #329: close the umbrella.
- #48 is superseded by #490/#491.

**Retitle to what is left:**

- #387: outline language coverage.
- #412: sd_notify READY.
- #424: per-child health and failure streaks.
- #441: `env.host` / `env.proc`.
- #453: residual piers edge cases.
- #472: document the partial-write tradeoff.
- #501: step two; man:// for moxy's own pages and the MANPATH ruling.
- #139: freud only.
- #187: arboretum md-toc via Lua.
- #216: shape 1 only.
- #230: name the wrapper's `--unset LD_LIBRARY_PATH`.
- #264: `restart <moxin>` only.
- #274: generic nested sandbox, or move it to clown.
- #278: gws-family bun tools.
- #386: moxins in the OCI image.
- #22: deny-tools, or close it.

## Close list (waits for the operator's confirmation)

These are the FIXED issues with nothing left over. Each cited commit was
checked to be on master (`git log origin/master`). The evidence that the
commit fixes the issue is the per-issue entry below. The comment column is
the close comment to post. Issues FIXED with something left over (#1, #216,
#387, #493, #501) are not on this list; they are retitle proposals above.

| # | commit(s) | close comment |
|---|---|---|
| 23 | d02b1f6 | HTTP MCP children (with OAuth) landed in d02b1f6; `url` in the server schema. |
| 82 | 00e4c6f, 9d5f3dc | `restart <moxin>` re-reads the moxin from disk (00e4c6f); `restart` re-reads MOXIN_PATH (9d5f3dc). |
| 129 | 8196dff, 0a0dadb | Cached results live in madder (8196dff), called in-process since 0a0dadb. |
| 140 | 57bd5ba | Dynamic perms shipped as `[dynamic-perms]` (57bd5ba), now used by folio, toolbox and others. |
| 162 | f946bcb | The toolbox moxin (FDR 0016, f946bcb) is the no-shell pipe over files and blob URIs. |
| 169 | 8196dff | Spilled results are madder blobs that folio.read reads, with start/end ranges. |
| 178 | 00e4c6f, fe47926, 4b3956a | Restart works for moxins (00e4c6f, fe47926); the reconnect test is 4b3956a. |
| 182 | 5d68484, 44495db | `async` takes a per-call `timeout` (5d68484, 44495db). |
| 186 | 0d11bd3 | Anchor extraction landed in 0d11bd3; it is `arboretum.md-anchor` today. |
| 211 | e72e5a6 | Unknown and missing-required arguments are rejected since e72e5a6. |
| 224 | efe1fb1 | Code outline is `arboretum.outline` (efe1fb1). |
| 225 | 43be0a1 | Structured find/replace is `arboretum.search` / `rewrite` (43be0a1). |
| 247 | 255845c | 255845c reverted 6ca4ab7; folio.read and folio.write are back. |
| 275 | d8f9bf7, 9ec501a | Child stdout is bounded and an oversize line self-heals (d8f9bf7, 9ec501a); auto-respawn is #409. |
| 323 | d6e2808 | The wake summary skips the truncation banner (d6e2808). |
| 330 | 120ca85, ae4a2d4 | grit's families are consolidated (120ca85, ae4a2d4); grit is 9 tools. |
| 375 | a004553 | `serve-http --name-template` gives dot-free names (a004553); resource→tool projection is #377. |
| 382 | 5d7a476 | The man moxin keeps an empty MANPATH entry so derived pages resolve (5d7a476). |
| 408 | c47532f | Persistent-child respawn is detached from the request context (c47532f). |
| 444 | 3300e22 | The godyn cutover (3300e22) pins tommy from the flake; the path in the issue is gone. |
| 485 | 2037822, cc57a76 | Serve-time enforcement for hookless clients (FDR 0015, 2037822). |
| 491 | 39f9341, cc57a76, d8e47ea | RFC 0002 and the policy-broker client landed (39f9341, cc57a76, d8e47ea). |
| 492 | 3cf92f1 | The policy-broker bats lane against a real socket landed in 3cf92f1. |
| 495 | 3a3a420, 6a7c4df, 7edf873 | hamster removed and always-allow reads constrained (3a3a420, 6a7c4df, 7edf873). |
| 497 | 5df5e35 | env inputs allowlisted, env_resolve gated and fenced (5df5e35). |
| 490 | cc57a76, 5ac99b4 | rg.search reads through folio's read rule (cc57a76, 5ac99b4). **Confirm with moxy/sunny-sumac, whose lane it is, before closing.** |

## READY items worked on 2026-11-06

Each landed item closed its issue on merge (`Closes #N`).

**Landed:**

- #136 (2e2de72), #339 (9323e3d).
- #231 (f8c2c71), #101 (75ad1e3), #176 (c7fa7e4).
- #394 (ae588b3), #389 (febb891), #195 (7f0e93f).
- #218 (a26351e), #468 (39e1b90).
- #412 (0980a2b). Switching krone's unit to `Type=notify` is circus's
  deploy.
- #264 (6adc2c6), #472 (cce107a).
- #447 (2ee7a8b). Not run against the live gws: it needs a Google login.
- #37 (c467b73), #439 (032e166), #116 (70d3341), #184 (bc88fff).

**Turned out not to be READY.** Each needs a ruling or work outside moxy:

- #431: the Go test lane has no madder store or binary. It needs madder's
  in-process init API (madder's lane) or a madder binary in the godyn
  test lane.
- #462: piers reads only GFM (`pandoc -f gfm`), which never produces cell
  spans, so merge code would be unreachable until piers takes another
  input format.
- #488: no verified PlantUML directive hides the gantt start/end/duration
  columns yet. Find the option first.
- #328, #329: folding the tools removes tool names that `allow-tools`
  lists and profiles may reference. Rule on the renames first.
- #438: the structural fix is moving the command layer onto dewey's
  `ActiveContext`, which the issue itself says needs a design pass.
- #469: rg.search's resources are #490's lane (moxy/sunny-sumac).
- #194: step 2 needs a bun/nixpkgs pin bump, which is a lock change.

## Rulings the READY work is waiting on

These READY issues can start once someone answers a small question:

- #504: which hook-log fields to keep.
- #450: enforce the annotation filter at call time, or document the gap.
- #406: re-probe, print a "run restart" hint, or wontfix.
- #426: lint or stream helper.
- #309: put the lint in moxy or in conformist.
- #302: mkMoxin as a stable public API.
- #267: batch blob output as a format argument or an automatic spill.
- #265: on a dynamic-perms crash, ask or return Unknown.
- #263: the default timeout.
- #476: delete the POC recipe.
- #437: per-connection HTTP sessions.
- #467: shared library or copy.
- #126: GitHub API dependency.
- #196: fix manix, or switch to another backend.
- #132: the logging key's name and merge rule.

These ready ones need no ruling, and each is size S:

- #101, #136, #176, #194, #195, #218, #231, #339, #389, #394, #431, #462,
  #468, #469 (but rg.search's resources are #490's lane).

## Cleanup opportunities (not issues; file:line)

- **README drift.** `README.md:97-113`, the shipped-moxins table:
  - env lists 5 tools, but the repo has 9;
  - grit lists 31, but has 9 (the families were folded);
  - man lists 4, but has 3;
  - chix (22) and arboretum (7) have no row;
  - moxins/toolbox's row is current.
- **AGENTS.md drift.**
  - `AGENTS.md:309`: the Folio section says folio is a native server in
    `.moxy/servers/folio.toml` "via inline shell commands" with three tools.
    That file does not exist. folio is `moxins/folio/` with 16 tools.
  - The same stale path appears at `docs/features/0003-freud.md:289`. That is
    a historical record, so leave it.
- **FDR 0016 drift.** `docs/features/0016-toolbox.md:188-189`, Out of scope:
  "Exposing the manpages as resources … a separate issue". #501 step one
  (87850b4) has since shipped this. The `toolbox(1)` path at `:45-46` still
  says the pages are installed only "on the man path".
- **Misleading comment.** `flake.nix:1124` says `generate-plugin` is
  purse-first's. The call is moxy's own go-mcp `App` method
  (`cmd/moxy/main.go:463`).
- **Misleading tool description.** `moxins/piers/update.toml:3` says
  "Fails without changing anything", but a multi-chunk failure leaves the
  earlier chunks applied (`moxins/piers/src/docs-markdown.ts:335-367`; #472).
- **Swallowed errors.** `internal/native/discovery.go:298` and
  `pkg/moxy/serving.go:121` ignore the `os.Getwd()` error, so relative
  MOXIN_PATH entries resolve against `""` without a warning (near #439).
- **Restart leaves stale state.** `pkg/moxy/bootstrap.go:268`
  (`ReloadMoxin`): a single-moxin restart calls neither `SetResolver` nor
  `SetManPageDirs`; only the full reload does (`pkg/moxy/instance.go:216-219`).
  The resolver half is #264. The man-page half is mine from 87850b4: a
  rebuilt moxin's new store path is not re-indexed until a full reload.
- **chix develop-run.** `moxins/chix/bin/develop-run:15` splits an arg that
  contains newlines (`mapfile` over `jq -r '.args[]'`). `:22` prints stderr
  only on failure. Both are #394.
- **freud.**
  - `moxins/freud/bin/interruptions:304`: `total = pc + ic` leaves out
    rejects (#136).
  - `moxins/freud/bin/sessions:89`: UTC timestamps with no label (#339).
- **gws-family bun tools.** `moxins/prison/src/get.ts:4,13` and their
  siblings ignore stderr and do not call `.nothrow()` (#278/#467).
- **moxy add.** `internal/add/add.go:196`: `strings.Fields` breaks quoted
  arguments, and the prompt at `:67` ("must be on $PATH") is misleading (#1).
- **No-process-exit lint not enforced.** `moxins/piers/src/images.ts:277` and
  `bin/*.mjs` call `process.exit` with no lint stopping them (#194 step 2).
- **TODO markers.** None lack an issue. The two tracked ones point at open
  issues: `cmd/moxy/main.go:477` TODO(#438) and `sweatfile:2`
  TODO(amarbel-llc/spinclass#19).
- **Exposure worth a ruling, not filed as a bug.** `freud.transcript` is
  always-allow with no gate (`docs/permissions-matrix.md:83`) and reads any
  session's transcript. That is a wider exposure than #504's hook logs, and
  so far it is mentioned only in a #496 comment.

## Per-issue verdicts

READY entries give the acceptance test, files, size (S under an hour, M half a
day, L several days) and whether a ruling is needed.

### #506–#437

| # | verdict | evidence / scope |
|---|---|---|
| 506 | READY | Acceptance: `string split --json` emits a JSON array; `match --json` emits groups with offsets. Files: `pkg/toolbox/string.go`, its tests, `toolbox-string.1.scd`. M. Ruling: no. |
| 505 | READY | Acceptance: `string escape --style url` and `unescape` round-trip; bad `%` or JSON escapes give an error, never a panic. Files: `pkg/toolbox/string.go`, `string_test.go`, the man page's NOT YET list. M. Ruling: no. |
| 504 | READY (ruling) | `internal/hook/hook.go:59,92` logs the full tool_input per session; `moxins/folio/bin/folio-perms:73` auto-allows `~/.local/log/moxy`; freud reads these logs. S–M. Ruling: which fields to keep. |
| 503 | DESIGN | `internal/proxy/proxy.go:1818-1824`: a moxin refused at startup is neither a child nor in `p.configs`, so restart says "unknown server". Ruling: treat a load error and a policy refusal differently? |
| 501 | FIXED (step one) | 87850b4 (`internal/proxy/resource_man.go`). Left: moxy's own pages, and the MANPATH ruling. Retitle or close. |
| 497 | FIXED | 5df5e35; `docs/permissions-matrix.md:54` shows env.resolve as broker+floor `path:read`. |
| 496 | DESIGN (part done) | 68f4bcc generates the matrix. Left: profile/role columns and a profile cross-link check, blocked on spinclass FDR 0034 and circus EPIC 0001 definitions. |
| 495 | FIXED | 3a3a420 (hamster removed), 6a7c4df, 7edf873. |
| 493 | FIXED | 569fbb5, c68a0c9. Left: revisit the 100 ms / 250 ms defaults against live data. |
| 492 | FIXED | 3cf92f1 (policy-broker bats lane against a real socket). |
| 491 | FIXED | 39f9341 (RFC 0002), cc57a76, d8e47ea; `pkg/policybroker`. |
| 490 | FIXED | cc57a76, 5ac99b4; `docs/permissions-matrix.md:109` shows rg.search as dynamic broker+floor. Lane: moxy/sunny-sumac. |
| 489 | DESIGN | `kind` is carried unchecked (afe13e2, ee22fe8), so the kind names are not final. Blocks circus D2/D3. |
| 488 | READY | Acceptance: a gantt block hides its date columns by default and keeps them with the option. Files: `moxins/piers/src`. S. Ruling: no. |
| 485 | FIXED | 2037822 (FDR 0015 enforce mode); `zz-tests_bats/server_side_perms.bats:100-138` is no longer skipped; `internal/proxy/proxy.go:1088`. |
| 483 | STALE | spinclass ships grit tools with a per-worktree stash shelf (spinclass#380). Move to spinclass. |
| 482 | DESIGN | `moxins/grit` ships alongside spinclass's equivalents. Ruling: deprecate, remove, or keep as the non-spinclass fallback. |
| 478 | BLOCKED | On igloo#87 (godyn-lint parity). |
| 477 | BLOCKED | On igloo#77/#78 (godyn cross-compilation). |
| 476 | READY | `justfile:368-382` (run-go-cover needs ambient go) and `justfile:1252` (POC). Acceptance: each runs through godyn or is deleted. S. Ruling: minor. |
| 475 | DESIGN | Blob-reader tool. Open: per-session blob scoping, line vs byte ranges, interaction with allow-tools and narrowing. |
| 474 | DESIGN | Docs API replaceImage investigation. Follows #465. |
| 473 | READY | Acceptance: `{#d4}` heading anchors survive `piers.update`. Files: `docs-requests.lua`, `docs-markdown.ts`. M. Ruling: wait for #465? |
| 472 | READY | e9168c9 already reports the failed chunk. Left: say in `update.toml`/`create.toml` that partial writes can happen. S. |
| 470 | BLOCKED | No release since #371 (v0.6.46 at 936dbbc predates 2926812). |
| 469 | READY | rg.search, folio.file-type and folio.tar lack `substitute-args`. Acceptance: rg.search on a blob URI finds the pattern. S. |
| 468 | READY | No `nextPageToken` in `moxins/piers`. Acceptance: a two-page stub returns a token, or every comment up to a cap. Files: `comment.ts`, `comment.toml`. S. |
| 467 | READY | car still calls bare zx `$`; the gws-family tools never got piers's scratch-dir handling. M. Ruling: shared library or copy. |
| 466 | DESIGN | Per-subcommand annotations: pick one of the issue's three options. |
| 465 | DESIGN | Extraction scope (the whole moxin or a converter library). Gates #462–#464, #473, #474. |
| 464 | READY | Carry footnotes and images beside the requests. M. Low priority; after #465. |
| 463 | READY | Bats fixtures for the three list cases in `docs-requests.lua`. M. Needs a Docs API check for per-level glyphs. |
| 462 | READY | Acceptance: a pandoc col_span emits `mergeTableCells`. S. |
| 458 | DUPLICATE | Of #467. |
| 453 | READY (residual) | Left: SIGTERM cleanup test, a conclusive plantuml-include test, an include allowlist; alt text is an API limit. S. |
| 452 | DESIGN | Blocked on research into madder media hosting on circus/krone. |
| 450 | READY | `matchesAnnotationFilter` runs only in ListToolsV1 (`internal/proxy/proxy.go:651,687`), not in `callToolV1` (`:1034-1100`). Acceptance: a call to a filtered tool is refused. S. Ruling: enforce or document. |
| 447 | READY | `moxins/car/src/export.ts:12-17` passes an absolute tmpdir to `--output`. Run gws from the tmp dir with a relative path. S. |
| 445 | DESIGN (umbrella) | prison, gmail, calendar and gws are untested; also open: other-platform hashes, validate-mcp, gws pin age. Split it, or keep it as a tracker. |
| 444 | FIXED | 3300e22 (godyn cutover); codegen uses the flake-pinned tommy (`justfile:60-82`). |
| 443 | DESIGN | Placeholder; blocked on spinclass FDR 0032. |
| 441 | READY (part done) | 41553f9 added cpu, loadavg, meminfo, uptime. Left: `env.host` (kvm, vmx/svm, DMI, nix system-features). M. |
| 440 | UNSURE | `flake.nix:292,573` still map x86_64-darwin; whether the systems list includes it needs `nix flake show` (not run). |
| 439 | READY | Reload calls `os.Getwd()` again (`pkg/moxy/serving.go:93`, `pkg/moxyfile/config.go:463`). Acceptance: remove the cwd, then restart succeeds. M. |
| 438 | READY | 5ff23df stopgap (`cmd/moxy/main.go:477`). Left: unwind instead of `os.Exit`. M. |
| 437 | READY | No `server/discover` in `internal/streamhttp`. Acceptance: the probe gets a valid response, not a 400. M. Ruling: per-connection sessions (b2ee119). |

### #436–#323

| # | verdict | evidence / scope |
|---|---|---|
| 436 | READY | `moxins/chix/bin/doc-options:7` rebuilds the manix index on every call. Acceptance: a `services.squid` lookup takes under 30 s; prebuild the index at build time. M. |
| 435 | STALE | The smith moxin left in 0d44f3b. Move to smith. |
| 434 | DESIGN | No `lib` export (`mkMoxin` at flake.nix:371, `mkBunMoxin` at :435). Same subject as #302. Its V0 adapter bug is now #507. |
| 433 | READY | `pkg/moxy/batch.go:111` rejects Unknown on the sync path. Acceptance: a sync batch with a no-perms tool runs and a deny still bails. Needs the hook to force `ask`, as 19f4750 did for async. S–M. |
| 432 | DESIGN | Policy is a description suffix only (`internal/native/server.go:184-185`). Possibly overtaken by FDR 0015 and RFC 0002. |
| 431 | READY | No Go test for `MadderClient.OpenBlob` (`internal/native/madder.go:193`). Acceptance: a missing digest errors within 2 s under -race. S. |
| 430 | DESIGN | Tool declaration vs an async "still running" state; pairs with #428. |
| 428 | STALE | just-us-agents retired (c007e8c); belongs in just-us. |
| 426 | READY | Acceptance: a gate check flags `out=$(...)` buffering in `moxins/*/bin`. M. Ruling: lint or helper. |
| 424 | DESIGN | The restart barrier landed in 3d6a3d6. Left: per-child health, failure streaks. |
| 415 | STALE | smith moxin removed (0d44f3b). |
| 412 | READY | Listener bound first (e9271fa, `cmd/moxy/serve.go:217-222`). Left: sd_notify READY. S. |
| 409 | READY | No `SetOnReadError`. Acceptance: an unrecoverable oversize line respawns the child. M. |
| 408 | FIXED | c47532f (`context.WithoutCancel`, `internal/proxy/proxy.go:1728,1814`). |
| 406 | READY | Frozen status string at `internal/proxy/proxy.go:707,1136`. S. Ruling: re-probe, hint, or wontfix. |
| 403 | DESIGN | Blocked on go-mcp support for server-initiated elicitation. |
| 402 | DESIGN | Canonical arg renames vs aliases; unknown args are now refused (e72e5a6). |
| 401 | DESIGN | Only `enforce-allow` exists (`pkg/moxyfile/schema/schema.go:37-40`); a per-server perms default needs a schema ruling. |
| 397 | STALE | smith moxin removed; sub-issues landed (dec8c9e, 393ad4d, 47e7961, c4de80a, ea65907). |
| 394 | READY | `moxins/chix/bin/develop-run:15,22`. Acceptance: a multi-line `bash -c` argument runs, and stderr shows even on success. S. |
| 393 | DESIGN | Superseded by #478 (flake.nix:1564-1566). |
| 391 | DESIGN | Interim opt-in through `gws-moxins` (flake.nix:1017-1055). Ruling: is that enough? |
| 389 | READY | No `--impure` in develop-run. Acceptance: `impure: true` enters an impure flake. S. |
| 387 | FIXED (mostly) | 790b497, a86916e, 0dd7548. Left: outline language coverage (`outline.ts:287`). |
| 386 | DESIGN | Blocked on an aarch64-linux builder; the maneater part is stale (6d17149). |
| 384 | DESIGN | FDR 0009 decision first; option 2 recommended. |
| 382 | FIXED | 5d7a476 (`--set-default MANPATH`, flake.nix:879-881). |
| 381 | DESIGN | Its trigger is handled by spinclass's detach window. Close or narrow. |
| 377 | DESIGN | Waits on clown#153. |
| 376 | DESIGN | Precedence between moxyfile keys and `--name-template`. L. |
| 375 | FIXED | a004553 (`--name-template`); the remainder is #377. |
| 369 | UNSURE | Candidates: b2ee119, 01a0b95 (HTTP session id). Not reproduced. |
| 368 | UNSURE | Same signature as #369; candidates 0a0dadb and a7a98a7. |
| 354 | STALE | sisyphus deleted (1b0f9f0). |
| 350 | READY | Acceptance: a grit reword subcommand rewrites a mid-stack message. M. |
| 349 | STALE | get-hubbed deleted (1b0f9f0). |
| 339 | READY | `moxins/freud/bin/sessions:89` prints unlabelled UTC. Acceptance: local time with offset. S. |
| 332 | STALE | get-hubbed deleted (1b0f9f0). |
| 330 | FIXED | 120ca85, ae4a2d4, c2292e6, 078bd47; grit is 9 tools. |
| 329 | READY | Fold the chix doc and store families into two tools; gc still prompts. M. |
| 328 | READY | Fold the freud timeseries tools into their base tools (`group_by`). S–M. |
| 323 | FIXED | d6e2808 (`internal/asyncjob/asyncjob.go:561-571`). |

### #320–#196

| # | verdict | evidence / scope |
|---|---|---|
| 320 | DESIGN (part done) | env, folio, piers and grit are folded; what is left is #328 and #329. Close the umbrella. |
| 309 | READY (phase 2) | Phase 1 landed in e848fa0. Acceptance: a lint flags `jq` inside `< <(…)`. M. Ruling: moxy or conformist. |
| 302 | READY | No `lib` output. Acceptance: an external flake calls `moxy.lib.${system}.mkMoxin`. S–M. Ruling: stable API? |
| 297 | DESIGN | Needs an FDR on marketplace.json ingestion. |
| 290 | STALE | just-us-agents retired (c007e8c). |
| 287 | STALE | `generate-plugin` is go-mcp's (`cmd/moxy/main.go:463`), so the purse-first CLI's removal does not touch moxy. |
| 286 | BLOCKED | On purse-first#120. |
| 279 | STALE | get-hubbed deleted. |
| 278 | READY (narrowed) | gws-family bun tools lack `.nothrow()` and drop stderr (`moxins/prison/src/get.ts:4,13`). S. |
| 275 | FIXED | d8f9bf7, 9ec501a (`internal/mcpclient/stdioreader.go`). Auto-respawn is #409. |
| 274 | DESIGN/BLOCKED | Nested sandbox; clown#94. |
| 273 | DESIGN | Dynamic tool lists need an FDR, and their use case retired. |
| 267 | READY | `internal/proxy/batch.go:231-243` stubs non-text blocks. Acceptance: the batch NDJSON carries a blob URI. M. Ruling: argument or auto-spill. |
| 266 | STALE premise | gomod2nix is gone (3300e22). Fold into #476. |
| 265 | READY | Crash or timeout → Ask (`internal/native/dynperms.go:99,106,148`). S–M. Ruling: document it, or return Unknown. |
| 264 | READY (narrowed) | Left: `restart <moxin>` (`pkg/moxy/bootstrap.go:268`) does not refresh the resolver. S. |
| 263 | READY | No per-tool `timeout-ms`. Acceptance: a hanging tool is killed and errors. M. Ruling: the default. |
| 257 | STALE | hamster removed. |
| 254 | DESIGN | A missing store is fatal (`pkg/moxy/instance.go:159`, `cmd/moxy/serve_moxin.go:78`). Ruling: auto-init, or run without blob caching. |
| 247 | FIXED | 255845c reverted 6ca4ab7. |
| 246 | STALE | conch dropped (0cbadd5). |
| 245 | DESIGN | Re-probing every ephemeral server is deliberate (`internal/proxy/proxy.go:1767`). Ruling needed. |
| 244 | DESIGN | Same tension: a restart is how a rebuilt binary is picked up. |
| 241 | STALE | sisyphus deleted. |
| 236 | STALE | get-hubbed perms gone. |
| 235 | STALE | get-hubbed deleted. |
| 234 | BLOCKED | `clown.json` still sits at the plugin root (flake.nix:1134-1135); waits on clown#32. |
| 233 | STALE | Superseded by `systemPromptPath` (750ae36). |
| 232 | STALE | Brew dropped (cf9fc4c). |
| 231 | READY | `SourceDir` exists (`internal/native/discovery.go:254`). Acceptance: tool scripts see `$MOXIN_DIR`. S. |
| 230 | DESIGN | The wrappers `--unset LD_LIBRARY_PATH` (flake.nix:410, 536). Ruling: keep, pass through, or make it opt-in. |
| 228 | STALE | gomarkdoc and gomod2nix are gone. |
| 225 | FIXED | 43be0a1 (arboretum search/rewrite; arboretum is unshipped). |
| 224 | FIXED | efe1fb1 (arboretum outline). |
| 218 | READY | Acceptance: push phase with B rejected and C skipped, in `zz-tests_bats/grit_push_stack.bats`. S. |
| 216 | FIXED (shape 2) | efe1fb1, 2d4375e. Shape 1 (a standalone app) was not delivered. |
| 213 | STALE | hamster removed. |
| 211 | FIXED | e72e5a6 (`internal/native/server.go:815-839`). |
| 210 | STALE | sisyphus deleted. |
| 206 | STALE | get-hubbed deleted. |
| 205 | STALE | sisyphus deleted. |
| 204 | STALE | sisyphus deleted. |
| 203 | STALE | sisyphus deleted. |
| 196 | READY (unsure) | Still manix-backed (`moxins/chix/bin/doc:12`); one live call came back empty. M. Ruling: fix manix or switch backend. |

### #195–#1

| # | verdict | evidence / scope |
|---|---|---|
| 195 | READY | `moxins/chix/src/flake-{check,lock,update}.ts` are near-identical. Acceptance: one shared runner, chix bats pass. S. |
| 194 | READY | Step 1 done (4638245). Step 2: enable `n/no-process-exit` and fix what it flags. S. |
| 193 | STALE | hamster removed. |
| 191 | DESIGN | Architecture question for clown (`stdioServers` or aggregation). |
| 188 | STALE | hamster removed; pandoc folded into arboretum (275c9be). |
| 187 | DESIGN | md-toc/md-section walk the AST in TS (`moxins/arboretum/src/md-toc.ts:11-37`). Lua filters? |
| 186 | FIXED | 0d11bd3 (`moxins/arboretum/md-anchor.toml`). |
| 184 | READY | No `strings` applet. Acceptance: `toolbox.run "strings bin"` lists printable runs under the read policy. S–M. |
| 182 | FIXED | `async` per-call `timeout` (5d68484, 44495db). |
| 179 | STALE | `internal/config/config_tommy.go` is gone; refile against tommy if still wanted. |
| 178 | FIXED | 00e4c6f, fe47926, 4b3956a; `zz-tests_bats/restart.bats:115,137`. |
| 176 | READY | `moxins/man/bin/list:5` uses `man -k .`, which needs an index. Acceptance: `man.list` shows a page from an unindexed MANPATH dir. S. |
| 174 | STALE | hamster removed. |
| 169 | FIXED | 8196dff; folio.read takes blob URIs (`moxins/folio/read.toml:9`). |
| 165 | DESIGN | Progressive disclosure; a POC exists in `zz-pocs/list-changed`, 92c7c05. |
| 162 | FIXED | toolbox (f946bcb, FDR 0016). |
| 161 | STALE | Tarballs removed (266e444). |
| 140 | FIXED | `[dynamic-perms]` (57bd5ba). |
| 139 | READY | The overflow is now a truncation banner plus a URI (`internal/native/cache.go:60`). Left: a freud overflow class. S. |
| 136 | READY | `moxins/freud/bin/interruptions:304` `total = pc + ic` leaves out rejects. Acceptance: each row's parts sum to its total. S. |
| 133 | DESIGN | SQL for freud: engine and guardrails. L. |
| 132 | READY | No logging keys (`pkg/moxyfile/schema/schema.go:18-50`). S–M. Ruling: the key's name and its merge rule. |
| 129 | FIXED | 8196dff, 0a0dadb (`internal/native/madder.go`). |
| 126 | READY | No nix-package-versions tool. M. Ruling: GitHub API dependency. |
| 124 | READY | No anchor pre-flight in piers. Acceptance: an edit that eats an anchor fails and names the comment. M. |
| 119 | DESIGN | Codex support scope. |
| 116 | READY | Acceptance: a bats lane diffs the bundle's tool names against a checked-in list. S–M. |
| 115 | STALE | hamster gone; `pathMode` (flake.nix:438, 834) covers the rest. |
| 113 | DESIGN | Synthetic MCP from CLI schemas. |
| 111 | STALE | Release tarballs removed (266e444). |
| 108 | DESIGN | `mkBunMoxin` still calls `buildBunBinaries` (flake.nix:469). Is buildZxScript still the target? |
| 107 | DESIGN | Needs an FDR first. |
| 106 | DESIGN | freud is still Python; the segfault was never root-caused. |
| 101 | READY | `internal/native/discovery.go:210-233` scans only child dirs. Acceptance: a MOXIN_PATH entry that is itself a moxin dir loads. S. |
| 96 | READY | Same as #263 (per-tool exec timeout). |
| 93 | STALE | The just-us-agents flake detection is gone (c007e8c). |
| 90 | READY | Oversized results per tool and session in freud. M. Overlaps #139. |
| 89 | READY | Time spent waiting on permission, in freud interruptions. M. |
| 88 | STALE | get-hubbed deleted. |
| 85 | DESIGN | Unscoped. |
| 82 | FIXED | 00e4c6f (`restart <moxin>` reloads from disk), 9d5f3dc. |
| 78 | STALE | just-us-agents retired. |
| 75 | STALE | go-mcp now comes from a flake input; the audit is moot. |
| 68 | STALE | No lux moxin here. |
| 62 | STALE | `test-validate` gone; the gate has `test-status-clean-env` (`justfile:259`). |
| 51 | STALE | One-off analysis; freud tool-frequency covers it. |
| 50 | READY | BurntSushi still imported (`internal/native/config.go:14`, `discovery.go:9`, `go.nix:55`). Acceptance: no import left, moxin load tests pass. M–L. |
| 48 | DESIGN | Superseded by #490/#491. |
| 46 | DESIGN | `moxy status` and `show-effective-moxyfile` cover part of it. |
| 42 | STALE | grit restructured as noun tools (FDR 0012). |
| 37 | READY | Child stderr goes to `os.Stderr` (`internal/mcpclient/client.go:58`). Acceptance: a crashing child's stderr tail appears in `FailedServer.Error`. M. |
| 23 | FIXED | d02b1f6 (`url` at `pkg/moxyfile/schema/schema.go:55`). |
| 22 | DESIGN | `allow-tools` exists (af016e4); there is no deny form. |
| 199 | STALE | No `nix_runner.rs` here; native exec gives children `/dev/null` as stdin (`internal/native/server.go:499-501`). |
| 4 | DESIGN | Partly covered by `permission-mode`/`enforce-allow` and `allow-tools`. |
| 1 | FIXED | `nix run …` and `npx …` work (`internal/add/add.go:74,196`); only quoting is missing. |
