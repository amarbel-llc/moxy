// Dump the full S-expression for a file, for debugging rule choices.
import { Parser, Language } from "web-tree-sitter";
import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const WASM = resolve(HERE, `node_modules/tree-sitter-wasms/out/tree-sitter-${process.argv[2]}.wasm`);

await Parser.init();
const lang = await Language.load(readFileSync(WASM));
const parser = new Parser();
parser.setLanguage(lang);
const src = readFileSync(process.argv[3], "utf8");
const tree = parser.parse(src);
if (!tree) {
  console.error("parse failed");
  process.exit(1);
}
console.log(tree.rootNode.toString());
