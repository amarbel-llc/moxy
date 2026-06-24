import { lstatSync, readdirSync, realpathSync, statSync } from "fs";
import { join } from "path";

const [path, long = "false"] = process.argv.slice(2);

const resolved = realpathSync(path);
if (!resolved.startsWith("/nix/store/")) {
  throw new Error(`path must be under /nix/store/, got: ${resolved}`);
}

const stat = statSync(resolved);
if (!stat.isDirectory()) {
  throw new Error(`not a directory: ${resolved}`);
}

interface Entry {
  name: string;
  type: "file" | "directory" | "symlink";
  size?: number | null;
}

// Enumerate via Node's fs (Dirent) rather than `find -printf`: the latter is a
// GNU findutils extension that BSD `find` (macOS) rejects with "-printf:
// unknown primary or operator" (#359, #360). withFileTypes gives the entry
// type without following symlinks, matching `find -printf %y`.
const entries: Entry[] = readdirSync(resolved, { withFileTypes: true })
  .map((dirent) => {
    const type: Entry["type"] = dirent.isDirectory()
      ? "directory"
      : dirent.isSymbolicLink()
        ? "symlink"
        : "file";
    if (long !== "true") {
      return { name: dirent.name, type };
    }
    // Preserve the prior shape: size only for regular files (lstat so a
    // symlink reports as a symlink, never followed), null otherwise.
    const size =
      type === "file" ? lstatSync(join(resolved, dirent.name)).size : null;
    return { name: dirent.name, type, size };
  })
  .sort((a, b) => a.name.localeCompare(b.name));

process.stdout.write(JSON.stringify(entries));
