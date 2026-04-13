import { $ } from "zx";
import { readdirSync, existsSync } from "fs";

$.verbose = false;

/** Go module cache uses !-prefixed lowercase for uppercase letters. */
function casefold(path: string): string {
  return path.replace(/[A-Z]/g, (ch) => `!${ch.toLowerCase()}`);
}

export interface ResolvedModule {
  modDir: string;
  subPkg: string;
}

/**
 * Resolve a Go package path to a module directory in GOMODCACHE.
 *
 * If the package includes @version, uses that version directly.
 * Otherwise walks path segments from longest to shortest prefix,
 * looking for cached module directories, and picks the newest version.
 */
export async function resolveMod(pkg: string): Promise<ResolvedModule> {
  const modcache = (await $`go env GOMODCACHE`).stdout.trim();
  if (!modcache) {
    throw new Error("GOMODCACHE not set");
  }

  // Handle explicit version: pkg@v1.0.0/sub/path
  let version: string | undefined;
  let pkgPath = pkg;
  const atIdx = pkg.indexOf("@");
  if (atIdx !== -1) {
    const afterAt = pkg.slice(atIdx + 1);
    const slashIdx = afterAt.indexOf("/");
    if (slashIdx === -1) {
      version = afterAt;
      pkgPath = pkg.slice(0, atIdx);
    } else {
      version = afterAt.slice(0, slashIdx);
      pkgPath = pkg.slice(0, atIdx) + afterAt.slice(slashIdx);
    }
  }

  // If we have an explicit version, try the exact module path first,
  // then walk up path segments.
  const segments = pkgPath.split("/");

  for (let i = segments.length; i >= 1; i--) {
    const prefix = segments.slice(0, i).join("/");
    const remainder = segments.slice(i).join("/");
    const folded = casefold(prefix);

    if (version) {
      const candidate = `${modcache}/${folded}@${version}`;
      if (existsSync(candidate)) {
        return { modDir: candidate, subPkg: remainder };
      }
    } else {
      // Glob for any version of this module
      const parent = `${modcache}/${folded.split("/").slice(0, -1).join("/")}`;
      const base = folded.split("/").at(-1)!;
      if (!existsSync(parent)) continue;

      const matches = readdirSync(parent)
        .filter((entry) => entry.startsWith(`${base}@`))
        .sort();

      if (matches.length > 0) {
        const newest = matches[matches.length - 1];
        return { modDir: `${parent}/${newest}`, subPkg: remainder };
      }
    }
  }

  throw new Error(`no cached module found for ${pkg}`);
}
