import { $, cd } from "zx";
import { resolveMod } from "./resolve-mod.ts";

$.verbose = false;

const [pkg, symbol, markdownStr, tags] = process.argv.slice(2);
const useMarkdown = markdownStr === "true";

// Substituted at nix build time via mkBunMoxin's extraSubstitutions. Brew
// builds and devshell runs leave the placeholder intact; the fallback below
// resolves the binary name on PATH instead.
const GOMARKDOC_SUBST = "@GOMARKDOC@";
const PANDOC_SUBST = "@PANDOC@";
const gomarkdocBin = GOMARKDOC_SUBST.startsWith("@")
  ? "gomarkdoc"
  : GOMARKDOC_SUBST;
const pandocBin = PANDOC_SUBST.startsWith("@") ? "pandoc" : PANDOC_SUBST;

async function resolveForGomarkdoc(p: string): Promise<string> {
  // gomarkdoc rejects bare import paths — it loads packages via go/packages
  // against the cwd's module, so it can only resolve local-on-disk paths.
  // Mirror what `go doc` does implicitly:
  //   - "./x" / "/abs/path" / "."  → pass through
  //   - "fmt", "encoding/json"     → $GOROOT/src/<pkg>     (stdlib)
  //   - "github.com/x/y/sub"       → resolveMod() → GOMODCACHE absolute path
  if (p === "." || p.startsWith("./") || p.startsWith("/")) return p;
  const firstSegment = p.split("/")[0];
  if (!firstSegment.includes(".")) {
    const goroot = (await $`go env GOROOT`.quiet()).stdout.trim();
    if (!goroot) throw new Error("GOROOT is empty; cannot resolve stdlib");
    return `${goroot}/src/${p}`;
  }
  const { modDir, subPkg } = await resolveMod(p);
  return subPkg ? `${modDir}/${subPkg}` : modDir;
}

// --- markdown-mode helpers (#188 symbol filtering via pandoc round-trip) ---

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function inlinesOf(block: any): any[] {
  if (!block || !block.c) return [];
  switch (block.t) {
    case "Para":
    case "Plain":
      return Array.isArray(block.c) ? block.c : [];
    case "Header":
      return Array.isArray(block.c) && Array.isArray(block.c[2])
        ? block.c[2]
        : [];
    default:
      return [];
  }
}

function blockHasNamedAnchor(block: any, name: string): boolean {
  const re = new RegExp(
    `<a\\s+(?:name|id)\\s*=\\s*["']${escapeRegex(name)}["']`,
  );
  for (const i of inlinesOf(block)) {
    if (
      i.t === "RawInline" &&
      Array.isArray(i.c) &&
      i.c[0] === "html" &&
      re.test(i.c[1] as string)
    ) {
      return true;
    }
  }
  return false;
}

const ANCHOR_NAME_RE = /<a\s+(?:name|id)\s*=\s*["']([^"']+)["']/;
function blockAnchorName(block: any): string | null {
  for (const i of inlinesOf(block)) {
    if (i.t === "RawInline" && Array.isArray(i.c) && i.c[0] === "html") {
      const m = ANCHOR_NAME_RE.exec(i.c[1] as string);
      if (m) return m[1];
    }
  }
  return null;
}

async function captureGomarkdoc(
  target: string,
  buildTags: string | undefined,
): Promise<string> {
  const args: string[] = ["-u"];
  if (buildTags) args.push("--tags", buildTags);
  args.push(target);
  const proc = Bun.spawn([gomarkdocBin, ...args], {
    stdout: "pipe",
    stderr: "inherit",
  });
  const out = await new Response(proc.stdout).text();
  const exitCode = await proc.exited;
  if (exitCode !== 0) throw new Error(`gomarkdoc exited ${exitCode}`);
  return out;
}

async function pandocPipe(
  inFmt: string,
  outFmt: string,
  input: string,
  extraArgs: string[] = [],
): Promise<string> {
  const proc = Bun.spawn(
    [pandocBin, "-f", inFmt, "-t", outFmt, ...extraArgs],
    { stdin: "pipe", stdout: "pipe", stderr: "pipe" },
  );
  proc.stdin.write(input);
  proc.stdin.end();
  const [stdout, exitCode, stderr] = await Promise.all([
    new Response(proc.stdout).text(),
    proc.exited,
    new Response(proc.stderr).text(),
  ]);
  if (exitCode !== 0) {
    throw new Error(`pandoc ${inFmt}→${outFmt} exited ${exitCode}: ${stderr}`);
  }
  return stdout;
}

if (useMarkdown) {
  // Experimental gomarkdoc backend — honors build tags via go/packages.
  // gomarkdoc/pandoc paths are baked in at build time via mkBunMoxin's
  // extraSubstitutions (see flake.nix); brew bundles fall back to PATH.
  let target: string;
  try {
    target = await resolveForGomarkdoc(pkg);
  } catch (err) {
    process.stderr.write(
      `doc (gomarkdoc): ${err instanceof Error ? err.message : err}\n`,
    );
    process.exit(1);
  }

  if (symbol) {
    // Symbol filter (moxy#188): capture gomarkdoc → pandoc gfm AST → slice
    // the block carrying `<a name="<symbol>">` through the next anchor block
    // → render back to gfm. Same algorithm as pandoc.anchor, inlined to
    // avoid cross-moxin runtime coupling.
    let markdown: string;
    try {
      markdown = await captureGomarkdoc(target, tags);
    } catch (err) {
      process.stderr.write(
        `doc (gomarkdoc): ${err instanceof Error ? err.message : err}\n`,
      );
      process.exit(1);
    }
    let ast: any;
    try {
      ast = JSON.parse(await pandocPipe("gfm", "json", markdown));
    } catch (err) {
      process.stderr.write(
        `doc (pandoc parse): ${err instanceof Error ? err.message : err}\n`,
      );
      process.exit(1);
    }
    const startIdx = ast.blocks.findIndex((b: any) =>
      blockHasNamedAnchor(b, symbol),
    );
    if (startIdx === -1) {
      const anchors: string[] = [];
      for (const block of ast.blocks) {
        const a = blockAnchorName(block);
        if (a) anchors.push(a);
      }
      process.stderr.write(
        `doc (markdown): symbol "${symbol}" not found in package "${pkg}"\n`,
      );
      if (anchors.length > 0) {
        const shown = anchors.slice(0, 20);
        process.stderr.write(
          `Available anchors (${anchors.length} total${anchors.length > shown.length ? `, showing first ${shown.length}` : ""}):\n`,
        );
        for (const a of shown) process.stderr.write(`  ${a}\n`);
      } else {
        process.stderr.write(`(package has no anchor blocks)\n`);
      }
      process.exit(1);
    }
    let endIdx = ast.blocks.length;
    for (let i = startIdx + 1; i < ast.blocks.length; i++) {
      if (blockAnchorName(ast.blocks[i]) !== null) {
        endIdx = i;
        break;
      }
    }
    const sliced = { ...ast, blocks: ast.blocks.slice(startIdx, endIdx) };
    try {
      const rendered = await pandocPipe(
        "json",
        "gfm",
        JSON.stringify(sliced),
        ["--wrap=none"],
      );
      process.stdout.write(rendered);
    } catch (err) {
      process.stderr.write(
        `doc (pandoc render): ${err instanceof Error ? err.message : err}\n`,
      );
      process.exit(1);
    }
    process.exit(0);
  }

  // No symbol: stream the whole package through. zx/Bun's `$` capture
  // truncates large outputs at ~8 KiB in the nix-bundled moxin (fmt is
  // ~80 KB), so we use Bun.spawn with stdio:"inherit". Diverges from the
  // zx-everywhere convention — see amarbel-llc/nixpkgs#11.
  const args: string[] = ["-u"];
  if (tags) args.push("--tags", tags);
  args.push(target);
  const proc = Bun.spawn([gomarkdocBin, ...args], {
    stdout: "inherit",
    stderr: "inherit",
  });
  const exitCode = await proc.exited;
  process.exit(exitCode);
}

function docArg(target: string, sym: string | undefined): string {
  return sym ? `${target}.${sym}` : target;
}

async function listSubPackages(target: string): Promise<string[]> {
  try {
    const result = await $`go list ${`${target}/...`}`.quiet();
    return result.stdout
      .split("\n")
      .map((l) => l.trim())
      .filter((l) => l && l !== target);
  } catch {
    return [];
  }
}

function withSubPackages(doc: string, subs: string[]): string {
  if (subs.length === 0) return doc;
  const trimmed = doc.replace(/\n+$/, "");
  const lines = subs.map((p) => `    ${p}`).join("\n");
  return `${trimmed}\n\nSub-packages (use hamster.doc on each for its API):\n${lines}\n`;
}

async function emit(doc: string, listTarget: string): Promise<void> {
  const subs = symbol ? [] : await listSubPackages(listTarget);
  process.stdout.write(withSubPackages(doc, subs));
}

// Try direct go doc first (handles stdlib and local module context).
// Use .quiet() so a failed first attempt doesn't leak stderr.
try {
  let target = pkg;
  try {
    const mod = (await $`go list -m`.quiet()).stdout.trim();
    if (mod && target !== mod && target.startsWith(`${mod}/`)) {
      target = `./${target.slice(mod.length + 1)}`;
    }
  } catch {
    // Not in a module — that's fine, continue with full path.
  }

  const arg = docArg(target, symbol);
  const result = await $`go doc -all ${arg}`.quiet();
  await emit(result.stdout, target);
  process.exit(0);
} catch {
  // Fall through to GOMODCACHE fallback.
}

// Fallback: resolve package in module cache and retry.
try {
  const { modDir, subPkg } = await resolveMod(pkg);
  cd(modDir);
  const target = subPkg ? `./${subPkg}` : ".";
  const arg = docArg(target, symbol);
  const result = await $`go doc -all ${arg}`;
  await emit(result.stdout, target);
} catch (err) {
  process.stderr.write(
    `doc: cannot find package "${pkg}"${symbol ? `.${symbol}` : ""}: ${err instanceof Error ? err.message : err}\n`,
  );
  process.exit(1);
}
