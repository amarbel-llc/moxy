import { $ } from "zx";
import { realpathSync, statSync } from "fs";

$.verbose = false;

const [path, long = "false"] = process.argv.slice(2);

const resolved = realpathSync(path);
if (!resolved.startsWith("/nix/store/")) {
  process.stderr.write(
    `Error: path must be under /nix/store/, got: ${resolved}\n`,
  );
  process.exit(1);
}

const stat = statSync(resolved);
if (!stat.isDirectory()) {
  process.stderr.write(`Error: not a directory: ${resolved}\n`);
  process.exit(1);
}

interface Entry {
  name: string;
  type: "file" | "directory" | "symlink";
  size?: number | null;
}

let entries: Entry[];

if (long === "true") {
  const out = (
    await $`find ${resolved} -maxdepth 1 -mindepth 1 -printf ${"%y\\t%s\\t%f\\n"}`
  ).stdout;
  entries = out
    .split("\n")
    .filter(Boolean)
    .map((line) => {
      const [typeChar, sizeStr, name] = line.split("\t");
      const type =
        typeChar === "d" ? "directory" : typeChar === "l" ? "symlink" : "file";
      return {
        name,
        type,
        size: typeChar === "f" ? parseInt(sizeStr) : null,
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
} else {
  const out = (
    await $`find ${resolved} -maxdepth 1 -mindepth 1 -printf ${"%y\\t%f\\n"}`
  ).stdout;
  entries = out
    .split("\n")
    .filter(Boolean)
    .map((line) => {
      const [typeChar, name] = line.split("\t");
      const type =
        typeChar === "d" ? "directory" : typeChar === "l" ? "symlink" : "file";
      return { name, type };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

process.stdout.write(JSON.stringify(entries));
