# Two permission systems, one authority: research note

Status: research note, 2026-11-10, moxy/live-aspen/koko. No code, no tool
annotated, no key chosen. Written for the operator's question: "it seems like
we have two different perms systems that we're trying to patch together instead
of unify".

Labels. **[observed]** I ran or listed it in this session. **[read]** I read it
in a document or source file this session; I have not run it. **[inferred]**
my reasoning from the above; treat as a theory to verify.

Sources read: moxy `docs/features/0015-serve-time-permission-enforcement.md`,
`docs/permissions-matrix.md` (generated), `AGENTS.md`/CLAUDE.md sections on
moxins, `pkg/perms` and `internal/proxy/proxy.go` (the funnel);
cutting-garden `docs/features/0028-ajour.md` ("The two inputs"); clown
`docs/rfcs/0018-capability-catalog-contract.md` §3, §9; circus
`docs/plans/2026-10-22-d1-...` §4 (rights) and `...-d2-profile-schema.md`.
Not read: the hook source (`internal/hook`), `folio-perms` source, clown RFC
0008/0019, spinclass's enforcement of profiles.

## (a) What moxy's permission system is today

1. **A tier per tool, declared by whoever writes the moxin.** Each moxin tool
   file has `perms-request`: `always-allow`, `each-use`, `delegate-to-client`
   (the default), or `dynamic` [read: AGENTS.md]. `dynamic` points at a
   `[dynamic-perms]` command (`folio-perms read|write|copy`) whose exit code is
   0 allow, 1 ask, 2 deny, decided per call from the arguments (path policy)
   [read]. Tools name the inputs that are resources in `[resources.<field>]`
   with an `op` [read].
2. **Counts** [observed, from the generated matrix]: 104 moxin tools: 71
   `always-allow`, 31 `dynamic`, 0 `each-use`, 2 `delegate-to-client`.
   Child-server tools ([[servers]] entries) have no tier at all (they are
   "unknown") [read: FDR 0015].
3. **Who enforces** [read: FDR 0015]. Under the default `delegate`, moxy's
   funnel resolves nothing; the Claude Code PreToolUse/PermissionRequest hook
   (`moxy hook`, a separate process) resolves the tier and prompts a human for
   ask. Under `permission-mode = "enforce"` the funnel resolves the tier itself:
   allow runs, and ask, deny and unknown are refused, because nobody can
   answer; `enforce-allow` lifts only "unknown". A policy broker
   (`CLOWN_POLICY_BROKER`, RFC 0002) can answer per resource first, and the
   tier's predicate is the "static floor" only when the broker does not answer
   [read: AGENTS.md pkg/policybroker].
4. **Narrowing** (FDR 0013): a child moxyfile may grant no more than its
   parent, including `allow-tools` and `permission-mode` [read].
5. **A second, unrelated classification already exists in the manifests:**
   `[annotations]`, MCP's tool hints (the matrix shows `read-only`,
   `destructive`, `idempotent`, `open-world`; AGENTS.md says the table holds
   exactly five keys, and I did not find the fifth) [observed: the matrix lists
   them per tool]. They are not used for any decision [inferred: the matrix lists them
   only under "hints"].
6. **Two things named `kind`** [read]: moxin(7) TOOL KIND, a free string for what
   a tool's result *is* (`list`, `issue`), emitted as `_meta.kind`
   [observed: `internal/native/server.go`]; no moxin declares it today
   [observed: `rg '^kind\s*='` over `moxins/*.toml`, no hits].

The tier is *not* an access class. Examples from the matrix [observed]:
`chix.build`, `chix.flake-update`, `chix.flake-lock` and `chix.run` are
`always-allow` though they run builds/update locks, and `folio.read` is
`dynamic` because its decision depends on the path. The tier answers "does this
call need a human", which depends on both the tool and its arguments [inferred].

## (b) What the ajour/profile system is

1. **A capability profile** is a hyphence+espalier document: a tree of plugin
   → server → tool nodes with predicate children, blessed by the operator's card
   (approval of the profile *is* the grant); absent means forbidden [read: D2 §1].
2. **Tool nodes carry atoms `kind=read|write|exec`, `perm=allow|ask`, `async`,
   `impure`**, "copied from the tool manifests at authoring and checked at
   launch; a mismatch refuses" (V-D2-11: `perm=allow` against a manifest's `ask`
   is refused) [read: D2 §1]. clown RFC 0018 §3 says the same: they are facts
   about the tool from its manifest, and pins [read].
3. **Rights at grant level**: `moxy:kind:read|write|exec` are "the tool-kind
   ceiling; the profile's tree says which tools" [read: D1 §4]. A profile that
   lists `folio/write` under a grant without `moxy:kind:write` is refused at
   bless time.
4. **Evaluation**: cutting-garden's `ajour.Evaluate(layers, call)` returns
   allow/deny/not-applicable; closed by default; an erroring layer is refused
   [read: FDR 0028]. The *enforcer* presents the call: moxy for moxy's tools
   (shadow mode now writes verdicts to a log and enforces nothing, moxy#489),
   a clown hook for harness built-ins, spinclass for its tools [read: D2 §8
   item 4].
5. **The source for `kind` and `perm` is moxy's manifest**, but no manifest
   declares `kind` in the read/write/exec sense, and clown works around it by
   parsing the `[perms: X]` description suffix and splitting the advertised name
   at the first "." [read: RFC 0018 §9 item 7].

## (c) Overlap and conflict

**Overlap** [inferred]: both answer "may this call run, and does it need a
human". Both are closed-by-default at their own level (no `perms-request` =
unknown/refused under enforce; no node = forbidden in a profile). Both are
narrowing-only down a chain (FDR 0013; profile cascade `_base`, D2 §1). Both are
enforced by moxy's funnel when moxy enforces.

**Conflicts / confusions**
1. **`kind` has two meanings** [observed/read]: moxy's `_meta.kind` is a result
   label; the profile's `kind=` is an access class. RFC 0018 §9 item 7 observed
   that the idiom `[-> _ !mcp-tool-v1 kind=read …]` therefore never admits a moxy
   call.
2. **`perm=` in the profile is a pin of `perms-request` in the manifest**
   [read]. So the profile does not decide permission; it must *repeat* the
   manifest's tier, and a mismatch refuses. The authority is the manifest, the
   profile is a checked copy. That is the reverse of the operator's end-state
   (the card-blessed profile is the authority) [inferred].
3. **`perm` cannot express `dynamic`**. A dynamic tool's verdict is per call
   (path). RFC 0018 maps `dynamic` to `perm=dynamic`, "not a pin"
   [read], so for 31 of 104 tools the profile's `perm` says nothing and moxy's
   predicate still decides. Two rule sets then apply to one call: the profile's
   predicates (paths via `fs-path-v1 subtree`) and `folio-perms`' path policy
   [inferred]. They can disagree; the combination rule (both must admit, as D1
   says for rights vs. tree [read]) is not written for tier vs. profile.
4. **Different ask semantics**. In moxy, ask = a human at a prompt, or a refusal
   under enforce. In an unattended profile `perm=ask` is forbidden
   (D2 §5 [read]); the profile cannot make ask → allow without lying about the
   manifest.
5. **Child-server tools have no tier**, so `perm` has no source for smith,
   chrest, nebulous etc., while the catalog lists them under `moxy` [observed:
   catalog.json]. Their `kind` would come from the child's own `_meta`.
6. **Three classifications of the same tool exist or are proposed**: the tier,
   MCP annotation hints, and a profile `kind`. Only the first affects behaviour
   today [inferred].
7. **Enforcement order is unwritten**: today hook → funnel (tier) → broker per
   resource; with ajour, a verdict from the profile too. Nobody has said which
   wins when they disagree [inferred; I did not read the hook].

## (d) A proposal that unifies them

Principle: **one record decides whether a call may run: the card-blessed
profile. Everything in the manifest is either a fact about the tool or a
default for when no profile applies.** [This is the operator's stated end-state;
the mechanism below is my design, not a ruling.]

1. **Split the manifest into facts and policy.**
   - *Facts* (stay in the manifest, written by the tool's author, reviewed in
     the moxy repo): the access class (`read|write|exec`), which input fields
     are paths/references and with which verb (today `substitute-args` and
     `[resources.*]`, which RFC 0018 §9 item 4 wants extended), `async`,
     `impure`, and the result label (rename today's `kind` to something like
     `result-label` so `kind` can have one meaning).
   - *Policy* (the thing the operator blesses): whether a given call is
     allowed, asked, or refused. Today this is `perms-request`.
2. **The profile becomes the authority for policy.** `perm=allow|ask` in the
   profile *is* the decision, not a copy to check against the manifest. The
   manifest check shrinks to "the profile names a tool that exists and its
   facts (kind, field kinds) match" (V-D2-9 style digest pin of the schema
   already exists for locking [read]). The grant's `moxy:kind:*` rights remain
   the ceiling, now meaningful because `kind` is a declared fact.
3. **`perms-request` becomes the default and the floor, derived where
   possible.** When no profile is in effect (today's interactive sessions, the
   hook, a bare `moxy serve`), the tier applies as now. It can be derived
   instead of hand-written for most tools: `read-only-hint` and
   `kind=read` with no open-world input → `always-allow`; a path-bearing tool →
   `dynamic` on its declared `[resources]` ops; the rest `delegate-to-client`.
   [inferred: I did not check that the 104 tools' current tiers match such a
   derivation; that is a first thing to measure.] The 31 `dynamic` tools keep
   their predicate as the floor *under* a profile (a profile cannot allow a path
   the path policy denies) or lose it in favour of `fs-path-v1` predicates;
   that is a decision, not a given (below).
4. **One reader.** moxy exposes the facts once (`tools/list` `_meta`, already
   approved for server/tool/perms-request in moxy#517, plus `kind` once its
   meaning is settled), and both the hook and ajour read from that, so the clown
   listing stops parsing description prose.
5. **One enforcement order, written down.** Closed by default: (1) shape gates
   (`--expose`, exclude, `allow-tools`), (2) the profile verdict if a profile
   is in effect; deny/absent → refuse, (3) the tier/floor only as a further
   restriction or, when no profile, as the decision, (4) the broker per
   resource. A call must pass every layer that exists (the D1 "both must admit"
   rule extended).

### What the operator would be deciding

1. **Does the profile's `perm=allow` override a manifest `ask`/`dynamic`?**
   - Option A (stricter, closer to today): no. The manifest tier is a floor
     under any profile. Unification is partial: one document of facts, two
     decisions, but written down and ordered.
   - Option B (the stated end-state): yes, within the grant's `kind` rights.
     The manifest tier becomes the no-profile default only. This needs the
     blessing step (card) to show the operator what `allow` overrides, because
     it removes a reviewer who is not the operator (the moxin author).
   My recommendation: B for `ask` (a human-prompt tier a card holder can
   pre-approve), A for `dynamic` path predicates until `fs-path-v1` can express
   everything `folio-perms` does; verify that claim before relying on it.
2. **Where `kind` comes from**: author-declared in each of 104 tool files
   (fact, reviewed), or derived from the existing `[annotations]` hints
   (no 104-file edit, but `exec` has no hint). Not chosen here.
3. **Child-server tools** (smith etc.): who supplies their facts? The child's
   `_meta`, a moxyfile overlay, or a moxy-side table (what the catalog copy is
   today).

### Smallest steps, none of them a commitment

- moxy#517: the three factual `_meta` keys (approved).
- Measure: derive a tier and a kind from `[annotations]` + `[resources]` for
  the 104 tools and diff against the declared tiers (a debug recipe; read-only).
- Write the enforcement order (point 5) as an FDR amendment to 0015 once the
  operator picks A or B.
- Keep shadow mode (moxy#489) as is: it needs only the facts, not the answer.

## Risks I see [inferred]

- Option B moves a safety decision from a code-reviewed manifest to a blessed
  document; the bless prompt must be able to show the effect or the card tap
  carries less meaning than the review it replaces.
- Renaming today's `kind` is a (small) wire change for any client reading
  `_meta.kind`; I found no moxin declaring one, so likely none in-repo, but I
  did not check clients outside this repo.
- Everything above about the hook and `folio-perms` comes from docs, not from
  reading their source.
