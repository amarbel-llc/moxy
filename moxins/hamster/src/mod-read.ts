import { existsSync, readFileSync, readdirSync } from "fs";
import { dirname, basename } from "path";
import { resolveMod } from "./resolve-mod.ts";

const [module, file, startStr, endStr] = process.argv.slice(2);

if (!module || !file) {
  throw new Error("usage: mod-read <module[@version]> <file> [start] [end]");
}

let modDir: string;
try {
  ({ modDir } = await resolveMod(module));
} catch (err) {
  throw new Error(`${err instanceof Error ? err.message : err}`);
}

const fullPath = `${modDir}/${file}`;
if (!existsSync(fullPath)) {
  const parent = dirname(fullPath);
  let detail = "";
  if (existsSync(parent)) {
    detail =
      `\n\navailable files in ${basename(parent)}:\n` +
      readdirSync(parent).join("\n");
  }
  throw new Error(`file not found: ${fullPath}${detail}`);
}

const content = readFileSync(fullPath, "utf-8");
const lines = content.split("\n");
// Remove trailing empty line from split if file ends with newline.
if (lines.length > 0 && lines[lines.length - 1] === "") {
  lines.pop();
}

const total = lines.length;
const start = startStr ? parseInt(startStr, 10) : undefined;
const end = endStr ? parseInt(endStr, 10) : undefined;

function formatLine(lineNum: number, text: string): string {
  return `${String(lineNum).padStart(6)}\t${text}\n`;
}

if (start !== undefined && end !== undefined) {
  for (let i = start - 1; i < Math.min(end, total); i++) {
    process.stdout.write(formatLine(i + 1, lines[i]));
  }
} else if (start !== undefined) {
  for (let i = start - 1; i < total; i++) {
    process.stdout.write(formatLine(i + 1, lines[i]));
  }
} else if (total > 2000) {
  process.stdout.write(`File has ${total} lines (showing head and tail).\n`);
  process.stdout.write("Use start/end params for specific sections.\n\n");
  process.stdout.write("--- Head ---\n");
  for (let i = 0; i < Math.min(50, total); i++) {
    process.stdout.write(formatLine(i + 1, lines[i]));
  }
  process.stdout.write("\n--- Tail ---\n");
  for (let i = Math.max(0, total - 20); i < total; i++) {
    process.stdout.write(formatLine(i + 1, lines[i]));
  }
} else {
  for (let i = 0; i < total; i++) {
    process.stdout.write(formatLine(i + 1, lines[i]));
  }
}
