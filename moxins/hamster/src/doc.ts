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

if (useMarkdown) {
  // Experimental gomarkdoc backend — honors build tags via go/packages.
  // Symbol-narrowing is not supported (gomarkdoc renders whole packages).
  // Path is pinned at nix build time via wrapper env var; falls back to
  // PATH so brew/devshell installs work too.
  if (symbol) {
    process.stderr.write(
      `note: symbol="${symbol}" is ignored in markdown mode (gomarkdoc has no symbol filter). ` +
        `Once moxy#186 lands, pipe this output through ` +
        `\`pandoc.select selector="#${symbol}"\` to extract one symbol's section.\n`,
    );
  }
  let target: string;
  try {
    target = await resolveForGomarkdoc(pkg);
  } catch (err) {
    process.stderr.write(
      `doc (gomarkdoc): ${err instanceof Error ? err.message : err}\n`,
    );
    process.exit(1);
  }
  const args: string[] = ["-u"];
  if (tags) args.push("--tags", tags);
  args.push(target);
  // Stream gomarkdoc's stdout/stderr straight through. zx/Bun's `$` capture
  // truncates large outputs at ~8 KiB in the nix-bundled moxin (real
  // packages routinely exceed this: fmt is ~80 KB), so we bypass the
  // captured-string path entirely. Diverges from the zx-everywhere convention
  // in other moxins — revert once amarbel-llc/nixpkgs#11 is understood.
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
