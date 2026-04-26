import { $ } from "zx";
import { resolveMod } from "./resolve-mod.ts";

$.verbose = false;

const [pkg, tagsArg, unexportedArg] = process.argv.slice(2);
const tags = tagsArg || undefined;
const includeUnexported = unexportedArg === "true";

// Substituted at nix build time via mkBunMoxin's extraSubstitutions; brew
// bundles fall back to PATH lookup via the startsWith guard.
const GOMARKDOC_SUBST = "@GOMARKDOC@";
const PANDOC_SUBST = "@PANDOC@";
const gomarkdocBin = GOMARKDOC_SUBST.startsWith("@")
  ? "gomarkdoc"
  : GOMARKDOC_SUBST;
const pandocBin = PANDOC_SUBST.startsWith("@") ? "pandoc" : PANDOC_SUBST;

async function resolveForGomarkdoc(p: string): Promise<string> {
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
): Promise<string> {
  const proc = Bun.spawn([pandocBin, "-f", inFmt, "-t", outFmt], {
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

function inlineText(inlines: any[]): string {
  return inlines
    .map((i: any) => {
      if (i.t === "Str") return i.c;
      if (i.t === "Space" || i.t === "SoftBreak") return " ";
      if (Array.isArray(i.c)) return inlineText(i.c);
      return "";
    })
    .join("");
}

function isExported(anchor: string): boolean {
  // Anchors like "Type.method" — exported if the first path segment starts
  // with an uppercase letter. Methods on unexported types are still
  // unexported even if the method name is uppercase.
  const head = anchor.split(".")[0];
  return /^[A-Z]/.test(head);
}

// --- Main ---

let target: string;
try {
  target = await resolveForGomarkdoc(pkg);
} catch (err) {
  process.stderr.write(
    `doc-outline: ${err instanceof Error ? err.message : err}\n`,
  );
  process.exit(1);
}

let markdown: string;
try {
  markdown = await captureGomarkdoc(target, tags);
} catch (err) {
  process.stderr.write(
    `doc-outline (gomarkdoc): ${err instanceof Error ? err.message : err}\n`,
  );
  process.exit(1);
}

let ast: any;
try {
  ast = JSON.parse(await pandocPipe("gfm", "json", markdown));
} catch (err) {
  process.stderr.write(
    `doc-outline (pandoc): ${err instanceof Error ? err.message : err}\n`,
  );
  process.exit(1);
}

// Walk blocks. For each anchor, look at the next block; if it's a Header,
// take its rendered text as context (e.g. "func Println", "type Stringer").
type Entry = { anchor: string; heading: string };
const entries: Entry[] = [];
for (let i = 0; i < ast.blocks.length; i++) {
  const a = blockAnchorName(ast.blocks[i]);
  if (a === null) continue;
  let heading = "";
  if (i + 1 < ast.blocks.length && ast.blocks[i + 1].t === "Header") {
    heading = inlineText(ast.blocks[i + 1].c[2]).trim();
  }
  entries.push({ anchor: a, heading });
}

const totalCount = entries.length;
const exportedCount = entries.filter((e) => isExported(e.anchor)).length;
const unexportedCount = totalCount - exportedCount;
const filtered = includeUnexported
  ? entries
  : entries.filter((e) => isExported(e.anchor));

// Pad anchor column for readability when the heading is shown.
const anchorWidth = filtered.reduce((w, e) => Math.max(w, e.anchor.length), 0);

const lines: string[] = [];
lines.push(`# ${pkg}`);
if (includeUnexported) {
  lines.push(`${totalCount} anchors`);
} else {
  lines.push(
    `${exportedCount} exported anchors${unexportedCount > 0 ? ` (${unexportedCount} unexported hidden; pass unexported=true to include)` : ""}`,
  );
}
lines.push("");
for (const e of filtered) {
  if (e.heading) {
    lines.push(`${e.anchor.padEnd(anchorWidth)}  # ${e.heading}`);
  } else {
    lines.push(e.anchor);
  }
}

process.stdout.write(lines.join("\n") + "\n");
process.exit(0);
