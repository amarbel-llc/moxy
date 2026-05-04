import { $ } from "zx";

$.verbose = false;

const [pattern, rewrite, pathArg, lang, globs, dryRunStr] = process.argv.slice(2);

if (!pattern || !rewrite) {
  process.stderr.write("usage: rewrite <pattern> <rewrite> [path] [lang] [globs] [dry_run]\n");
  process.exit(2);
}

const dryRun = dryRunStr === "true";
const targetPath = pathArg && pathArg.length > 0 ? pathArg : ".";

const baseArgs: string[] = ["run", "--pattern", pattern, "--rewrite", rewrite];
if (lang) baseArgs.push("--lang", lang);
if (globs) baseArgs.push("--globs", globs);

// First pass: capture the diff for the caller (no -U). ast-grep prints the
// proposed change without touching disk when --update-all is omitted.
const previewArgs = [...baseArgs, targetPath];
const preview = await $`ast-grep ${previewArgs}`.quiet().nothrow();

if (preview.exitCode !== 0 && preview.exitCode !== 1) {
  process.stderr.write(preview.stderr);
  process.exit(preview.exitCode ?? 1);
}

if (dryRun) {
  process.stdout.write(preview.stdout);
  if (preview.stdout.length === 0) process.stdout.write("(no matches)\n");
  process.exit(0);
}

// Second pass: actually apply. -U skips the interactive confirmation.
const applyArgs = [...baseArgs, "--update-all", targetPath];
const apply = await $`ast-grep ${applyArgs}`.quiet().nothrow();

if (apply.exitCode !== 0 && apply.exitCode !== 1) {
  process.stderr.write(apply.stderr);
  process.exit(apply.exitCode ?? 1);
}

// Caller wants to see what changed; preview's stdout is the diff.
process.stdout.write(preview.stdout);
if (preview.stdout.length === 0) process.stdout.write("(no matches)\n");
// ast-grep prints "Applied N changes" on -U success. Versions vary on
// which stream the message lands on (some send it to both), so prefer
// stdout and only fall back to stderr.
const footer = (apply.stdout.trim() || apply.stderr.trim());
if (footer) process.stdout.write(`\n${footer}\n`);
