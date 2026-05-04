// Phase 1 probe: can we load web-tree-sitter under bun and parse a Go snippet?
import { Parser, Language } from "web-tree-sitter";
import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const GO_WASM = resolve(
  HERE,
  "node_modules/tree-sitter-wasms/out/tree-sitter-go.wasm",
);

await Parser.init();
const lang = await Language.load(readFileSync(GO_WASM));
const parser = new Parser();
parser.setLanguage(lang);

const src = `package main

import "fmt"

func main() {
  fmt.Println("hello")
}

type Foo struct {
  Bar int
}

func (f Foo) Baz() string { return "baz" }
`;

const tree = parser.parse(src);
if (!tree) {
  console.log("FAIL: parse returned null");
  process.exit(1);
}
console.log(tree.rootNode.toString());
console.log("PASS");
