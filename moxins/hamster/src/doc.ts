import { $ } from "zx";
import { resolveMod } from "./resolve-mod.ts";

$.verbose = false;

const [pkg, symbol, tags] = process.argv.slice(2);

// Substituted at nix build time via mkBunMoxin's extraSubstitutions. Brew
// builds and devshell runs leave the placeholder intact; the startsWith
// guard falls back to PATH lookup for those cases.
const GOMARKDOC_SUBST = "@GOMARKDOC@";
const PANDOC_SUBST = "@PANDOC@";
const gomarkdocBin = GOMARKDOC_SUBST.startsWith("@")
  ? "gomarkdoc"
  : GOMARKDOC_SUBST;
const pandocBin = PANDOC_SUBST.startsWith("@") ? "pandoc" : PANDOC_SUBST;

async function resolveForGomarkdoc(p: string): Promise<string> {
  // gomarkdoc rejects bare import paths — it loads packages via go/packages
  // against the cwd's module, so it can only resolve local-on-disk paths.
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

// --- pandoc AST helpers (symbol filtering via gfm round-trip) ---

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
  const result = await $`${gomarkdocBin} ${args}`.quiet();
  return result.stdout;
}

async function pandocPipe(
  inFmt: string,
  outFmt: string,
  input: string,
  extraArgs: string[] = [],
): Promise<string> {
  const proc = Bun.spawn([pandocBin, "-f", inFmt, "-t", outFmt, ...extraArgs], {
    stdin: "pipe",
    stdout: "pipe",
    stderr: "pipe",
  });
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

async function listSubPackages(targetPkg: string): Promise<string[]> {
  // Use the user's original `pkg` argument (e.g. "fmt", "./internal/x",
  // "github.com/x/y") rather than the resolved filesystem path — `go list`
  // resolves the same way the user typed it.
  try {
    const result = await $`go list ${`${targetPkg}/...`}`.quiet();
    return result.stdout
      .split("\n")
      .map((l) => l.trim())
      .filter((l) => l && l !== targetPkg);
  } catch {
    return [];
  }
}

function appendSubPackagesSection(markdown: string, subs: string[]): string {
  if (subs.length === 0) return markdown;
  const trimmed = markdown.replace(/\n+$/, "");
  const items = subs.map((p) => `- ${p}`).join("\n");
  return `${trimmed}\n\n## Sub-packages\n\nUse \`hamster.doc\` on each for its API.\n\n${items}\n`;
}

async function main(): Promise<number> {
  let target: string;
  try {
    target = await resolveForGomarkdoc(pkg);
  } catch (err) {
    process.stderr.write(`doc: ${err instanceof Error ? err.message : err}\n`);
    return 1;
  }

  if (symbol) {
    // Symbol filter: capture gomarkdoc → pandoc gfm AST → slice the block
    // carrying `<a name="<symbol>">` through the next anchor block → render
    // back to gfm. Same algorithm as arboretum.md-anchor, inlined to avoid
    // cross-moxin runtime coupling.
    let markdown: string;
    try {
      markdown = await captureGomarkdoc(target, tags);
    } catch (err) {
      process.stderr.write(
        `doc (gomarkdoc): ${err instanceof Error ? err.message : err}\n`,
      );
      return 1;
    }
    let ast: any;
    try {
      ast = JSON.parse(await pandocPipe("gfm", "json", markdown));
    } catch (err) {
      process.stderr.write(
        `doc (pandoc parse): ${err instanceof Error ? err.message : err}\n`,
      );
      return 1;
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
        `doc: symbol "${symbol}" not found in package "${pkg}"\n`,
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
      return 1;
    }
    let endIdx = ast.blocks.length;
    for (let i = startIdx + 1; i < ast.blocks.length; i++) {
      if (blockAnchorName(ast.blocks[i]) !== null) {
        endIdx = i;
        break;
      }
    }
    const sliced = { ...ast, blocks: ast.blocks.slice(startIdx, endIdx) };
    let rendered: string;
    try {
      rendered = await pandocPipe("json", "gfm", JSON.stringify(sliced), [
        "--wrap=none",
      ]);
    } catch (err) {
      process.stderr.write(
        `doc (pandoc render): ${err instanceof Error ? err.message : err}\n`,
      );
      return 1;
    }
    process.stdout.write(rendered);
    return 0;
  }

  // Whole package: capture gomarkdoc + append a Sub-packages section so
  // callers can discover the module surface in one query.
  let markdown: string;
  try {
    markdown = await captureGomarkdoc(target, tags);
  } catch (err) {
    process.stderr.write(
      `doc (gomarkdoc): ${err instanceof Error ? err.message : err}\n`,
    );
    return 1;
  }

  const subs = await listSubPackages(pkg);
  process.stdout.write(appendSubPackagesSection(markdown, subs));
  return 0;
}

process.exitCode = await main();
