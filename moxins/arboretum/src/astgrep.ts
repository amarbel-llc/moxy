// Shared helpers for arboretum.search and arboretum.rewrite. Both shell out
// to ast-grep and need the same pattern-disambiguation logic for languages
// where bare `--pattern` is parser-ambiguous.

// In Go, `fmt.Println($X)` parses as a `type_conversion_expression` rather
// than a `call_expression` because Go's grammar allows `T(x)` as a type
// conversion (e.g. `int(3.14)`). The fix is to wrap the pattern in a
// function body and pin the selector to call_expression — see
// https://ast-grep.github.io/catalog/go/#match-function-call-in-golang
//
// We only auto-wrap when the pattern looks like a call expression (contains
// parens). Other Go patterns (declarations, statements, types) parse fine
// bare. Patterns the heuristic doesn't recognize fall through to plain
// `--pattern` and behave exactly as before.
function looksLikeGoCall(pattern: string): boolean {
  return pattern.includes("(") && pattern.includes(")");
}

export type AstGrepInvocation = {
  // Subcommand: "run" for plain --pattern flow, "scan" for --inline-rules.
  subcommand: "run" | "scan";
  // argv past the subcommand, *not* including --update-all or path.
  args: string[];
};

// Build the ast-grep invocation for a search-style call (no rewrite).
export function buildSearchInvocation(opts: {
  pattern: string;
  lang?: string;
  globs?: string;
  context?: string; // -C N value
  outputMode?: string; // "json" -> --json=stream
}): AstGrepInvocation {
  if (opts.lang === "go" && looksLikeGoCall(opts.pattern)) {
    const rule = goCallRule({ pattern: opts.pattern });
    const args: string[] = ["--inline-rules", rule];
    if (opts.globs) args.push("--globs", opts.globs);
    if (opts.context) args.push("-C", opts.context);
    if (opts.outputMode === "json") args.push("--json=stream");
    return { subcommand: "scan", args };
  }
  const args: string[] = ["--pattern", opts.pattern];
  if (opts.lang) args.push("--lang", opts.lang);
  if (opts.globs) args.push("--globs", opts.globs);
  if (opts.context) args.push("-C", opts.context);
  if (opts.outputMode === "json") args.push("--json=stream");
  return { subcommand: "run", args };
}

// Build the ast-grep invocation for a rewrite-style call.
export function buildRewriteInvocation(opts: {
  pattern: string;
  rewrite: string;
  lang?: string;
  globs?: string;
}): AstGrepInvocation {
  if (opts.lang === "go" && looksLikeGoCall(opts.pattern)) {
    const rule = goCallRule({ pattern: opts.pattern, fix: opts.rewrite });
    const args: string[] = ["--inline-rules", rule];
    if (opts.globs) args.push("--globs", opts.globs);
    return { subcommand: "scan", args };
  }
  const args: string[] = [
    "--pattern", opts.pattern,
    "--rewrite", opts.rewrite,
  ];
  if (opts.lang) args.push("--lang", opts.lang);
  if (opts.globs) args.push("--globs", opts.globs);
  return { subcommand: "run", args };
}

// Synthesize a Go-call YAML rule as JSON. JSON is a valid subset of YAML, so
// passing JSON through --inline-rules avoids the headache of escaping a
// caller-controlled pattern as a YAML scalar. The rule wraps the user's
// pattern in `func t() { ... }` (a function body context) and pins the
// selector to call_expression so tree-sitter-go's call-vs-conversion
// ambiguity resolves to call.
function goCallRule(opts: { pattern: string; fix?: string }): string {
  const rule: Record<string, unknown> = {
    id: "arboretum-inline",
    language: "go",
    rule: {
      pattern: {
        context: `func t() { ${opts.pattern} }`,
        selector: "call_expression",
      },
    },
  };
  if (opts.fix !== undefined) rule.fix = opts.fix;
  return JSON.stringify(rule);
}
