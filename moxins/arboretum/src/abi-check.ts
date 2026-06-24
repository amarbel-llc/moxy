// Drift gate: assert every vendored tree-sitter grammar's ABI version is
// within the running web-tree-sitter's supported range, and that the runtime
// wasm the bundle loads actually exists.
//
// Why this exists: the grammar wasm files (moxins/arboretum/wasm/) and the
// web-tree-sitter runtime are pinned independently. tree-sitter is backwards-
// but not forwards-compatible, so a grammar built for a newer ABI than the
// runtime supports can fault deep in the parser. This check turns that drift
// into a clear, gated failure at merge time (moxy#379). It is run by
// zz-tests_bats/arboretum_abi_check.bats.
//
// NOTE: an in-range ABI is necessary but NOT sufficient — moxy#379's bash
// `case` crash is a wasm symbol-bind failure with an in-range grammar. The
// scanner-exercising golden fixtures cover that behavioral class; this tool
// covers the structural ABI range + runtime-wasm presence.

import {
  Parser,
  Language,
  LANGUAGE_VERSION,
  MIN_COMPATIBLE_VERSION,
} from "web-tree-sitter";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { join, basename } from "node:path";

// @WASM_DIR@ is replaced pre-bundle by mkBunMoxin with the moxin's vendored
// wasm dir (same convention as outline.ts).
const WASM_DIR = "@WASM_DIR@";

// web-tree-sitter loads its runtime wasm via locateFile at init; the filename
// is part of the version contract (it was renamed tree-sitter.wasm ->
// web-tree-sitter.wasm across a web-tree-sitter major). outline.ts asks for
// "tree-sitter.wasm", so that file must exist in WASM_DIR.
const RUNTIME_WASM = "tree-sitter.wasm";

await Parser.init({
  locateFile(scriptName: string) {
    return `${WASM_DIR}/${scriptName}`;
  },
});

const problems: string[] = [];
const lines: string[] = [];

lines.push(
  `runtime web-tree-sitter ABI range: [${MIN_COMPATIBLE_VERSION}, ${LANGUAGE_VERSION}]`,
);

if (!existsSync(join(WASM_DIR, RUNTIME_WASM))) {
  problems.push(
    `runtime wasm ${RUNTIME_WASM} missing from ${WASM_DIR} — outline's ` +
      `Parser.init locateFile expects it; web-tree-sitter may have renamed it.`,
  );
}

const grammarWasms = readdirSync(WASM_DIR)
  .filter((f) => f.startsWith("tree-sitter-") && f.endsWith(".wasm"))
  .sort();

if (grammarWasms.length === 0) {
  problems.push(`no grammar wasm files found in ${WASM_DIR}`);
}

for (const file of grammarWasms) {
  const name = basename(file)
    .replace(/^tree-sitter-/, "")
    .replace(/\.wasm$/, "");
  try {
    const lang = await Language.load(readFileSync(join(WASM_DIR, file)));
    const abi = lang.abiVersion;
    const inRange = abi >= MIN_COMPATIBLE_VERSION && abi <= LANGUAGE_VERSION;
    lines.push(
      `  ${name}: abiVersion=${abi} ${inRange ? "ok" : "OUT OF RANGE"}`,
    );
    if (!inRange) {
      problems.push(
        `${name}: abiVersion ${abi} outside supported range ` +
          `[${MIN_COMPATIBLE_VERSION}, ${LANGUAGE_VERSION}]`,
      );
    }
  } catch (err) {
    lines.push(`  ${name}: LOAD FAILED`);
    problems.push(`${name}: failed to load — ${(err as Error).message}`);
  }
}

process.stdout.write(lines.join("\n") + "\n");

if (problems.length > 0) {
  process.stderr.write(
    "\ngrammar/runtime ABI drift detected:\n" +
      problems.map((p) => `  - ${p}`).join("\n") +
      "\nRebuild the vendored grammars against a matching tree-sitter, or " +
      "realign web-tree-sitter. See moxy#379.\n",
  );
  process.exitCode = 1;
}
