import { $, cd } from "zx";
import { resolveMod } from "./resolve-mod.ts";

$.verbose = false;

const [pkg, symbol] = process.argv.slice(2);

function docArg(target: string, sym: string | undefined): string {
  return sym ? `${target}.${sym}` : target;
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
  process.stdout.write(result.stdout);
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
  process.stdout.write(result.stdout);
} catch (err) {
  process.stderr.write(
    `doc: cannot find package "${pkg}"${symbol ? `.${symbol}` : ""}: ${err instanceof Error ? err.message : err}\n`,
  );
  process.exit(1);
}
