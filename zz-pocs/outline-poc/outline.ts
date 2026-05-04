// outline.ts — POC: hierarchical source outlines via tree-sitter.
//
// Hardcoded constants at the top. No CLI flags. Single positional arg = file.
// Phase 2 supports Go only; Phase 3 fans out.

import { Parser, Language, type Node, type Tree } from "web-tree-sitter";
import { readFileSync, statSync, readdirSync } from "node:fs";
import { extname, join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const WASM_DIR = resolve(HERE, "node_modules/tree-sitter-wasms/out");

// Map: file extension -> language config.
//
// `nodes` lists the tree-sitter node types we render in the outline. `name`
// gives the field path or child query used to extract the symbol name.
//
// Keep this dumb and hardcoded; no query (.scm) files for the POC.
type NodeRule = {
  // tree-sitter node type.
  type: string;
  // user-facing kind label.
  kind: string;
  // How to find the name node within this node.
  // - {field: "name"} -> node.childForFieldName("name")
  // - {child: "identifier"} -> first descendant of that type
  name: { field?: string; child?: string };
};

type LangConfig = {
  wasm: string;
  rules: NodeRule[];
};

const LANGS: Record<string, LangConfig> = {
  ".go": {
    wasm: `${WASM_DIR}/tree-sitter-go.wasm`,
    rules: [
      { type: "function_declaration", kind: "func", name: { field: "name" } },
      { type: "method_declaration", kind: "method", name: { field: "name" } },
      // The outer `type_declaration` / `const_declaration` / `var_declaration`
      // nodes wrap their `*_spec` children with the keyword and would
      // duplicate the entry, so we list only the inner specs.
      { type: "type_spec", kind: "type", name: { field: "name" } },
      { type: "const_spec", kind: "const", name: { child: "identifier" } },
      { type: "var_spec", kind: "var", name: { child: "identifier" } },
      { type: "field_declaration", kind: "field", name: { field: "name" } },
      // `method_spec` in older grammar releases, `method_elem` in newer.
      // The wasm bundled by tree-sitter-wasms uses `method_spec`.
      { type: "method_spec", kind: "method", name: { field: "name" } },
      { type: "method_elem", kind: "method", name: { field: "name" } },
    ],
  },
  ".js": {
    wasm: `${WASM_DIR}/tree-sitter-javascript.wasm`,
    rules: [
      { type: "function_declaration", kind: "func", name: { field: "name" } },
      { type: "class_declaration", kind: "class", name: { field: "name" } },
      { type: "method_definition", kind: "method", name: { field: "name" } },
      { type: "lexical_declaration", kind: "let", name: { child: "identifier" } },
      { type: "variable_declaration", kind: "var", name: { child: "identifier" } },
    ],
  },
  ".ts": {
    wasm: `${WASM_DIR}/tree-sitter-typescript.wasm`,
    rules: [
      { type: "function_declaration", kind: "func", name: { field: "name" } },
      { type: "class_declaration", kind: "class", name: { field: "name" } },
      { type: "interface_declaration", kind: "interface", name: { field: "name" } },
      { type: "type_alias_declaration", kind: "type", name: { field: "name" } },
      { type: "enum_declaration", kind: "enum", name: { field: "name" } },
      { type: "method_signature", kind: "method", name: { field: "name" } },
      { type: "method_definition", kind: "method", name: { field: "name" } },
      { type: "property_signature", kind: "field", name: { field: "name" } },
      { type: "public_field_definition", kind: "field", name: { field: "name" } },
      { type: "lexical_declaration", kind: "let", name: { child: "identifier" } },
    ],
  },
  ".tsx": {
    wasm: `${WASM_DIR}/tree-sitter-tsx.wasm`,
    rules: [
      { type: "function_declaration", kind: "func", name: { field: "name" } },
      { type: "class_declaration", kind: "class", name: { field: "name" } },
      { type: "interface_declaration", kind: "interface", name: { field: "name" } },
      { type: "type_alias_declaration", kind: "type", name: { field: "name" } },
      { type: "enum_declaration", kind: "enum", name: { field: "name" } },
      { type: "method_definition", kind: "method", name: { field: "name" } },
      { type: "lexical_declaration", kind: "let", name: { child: "identifier" } },
    ],
  },
  ".rs": {
    wasm: `${WASM_DIR}/tree-sitter-rust.wasm`,
    rules: [
      { type: "function_item", kind: "fn", name: { field: "name" } },
      { type: "struct_item", kind: "struct", name: { field: "name" } },
      { type: "enum_item", kind: "enum", name: { field: "name" } },
      { type: "trait_item", kind: "trait", name: { field: "name" } },
      { type: "impl_item", kind: "impl", name: { child: "type_identifier" } },
      { type: "mod_item", kind: "mod", name: { field: "name" } },
      { type: "const_item", kind: "const", name: { field: "name" } },
      { type: "static_item", kind: "static", name: { field: "name" } },
      { type: "type_item", kind: "type", name: { field: "name" } },
      { type: "field_declaration", kind: "field", name: { field: "name" } },
      { type: "enum_variant", kind: "variant", name: { field: "name" } },
    ],
  },
  ".py": {
    wasm: `${WASM_DIR}/tree-sitter-python.wasm`,
    rules: [
      { type: "function_definition", kind: "def", name: { field: "name" } },
      { type: "class_definition", kind: "class", name: { field: "name" } },
      { type: "decorated_definition", kind: "decorated", name: { child: "identifier" } },
    ],
  },
  ".php": {
    wasm: `${WASM_DIR}/tree-sitter-php.wasm`,
    rules: [
      { type: "function_definition", kind: "func", name: { field: "name" } },
      { type: "class_declaration", kind: "class", name: { field: "name" } },
      { type: "interface_declaration", kind: "interface", name: { field: "name" } },
      { type: "trait_declaration", kind: "trait", name: { field: "name" } },
      { type: "method_declaration", kind: "method", name: { field: "name" } },
      { type: "property_declaration", kind: "field", name: { child: "variable_name" } },
      { type: "const_declaration", kind: "const", name: { child: "name" } },
    ],
  },
  ".sh": {
    wasm: `${WASM_DIR}/tree-sitter-bash.wasm`,
    rules: [
      { type: "function_definition", kind: "func", name: { field: "name" } },
    ],
  },
  ".bash": {
    wasm: `${WASM_DIR}/tree-sitter-bash.wasm`,
    rules: [
      { type: "function_definition", kind: "func", name: { field: "name" } },
    ],
  },
};

function findName(node: Node, rule: NodeRule): string {
  if (rule.name.field) {
    const n = node.childForFieldName(rule.name.field);
    if (n) return n.text;
  }
  if (rule.name.child) {
    // First descendant of the named type.
    const cursor = node.walk();
    const visit = (): string | null => {
      do {
        const c = cursor.currentNode;
        if (c.type === rule.name.child) return c.text;
        if (cursor.gotoFirstChild()) {
          const r = visit();
          if (r) return r;
          cursor.gotoParent();
        }
      } while (cursor.gotoNextSibling());
      return null;
    };
    cursor.gotoFirstChild();
    const found = visit();
    if (found) return found;
  }
  return "<anonymous>";
}

type OutlineNode = {
  kind: string;
  name: string;
  startLine: number; // 1-indexed
  endLine: number;
  children: OutlineNode[];
};

function buildOutline(root: Node, rules: NodeRule[]): OutlineNode[] {
  const ruleByType = new Map(rules.map((r) => [r.type, r]));
  const out: OutlineNode[] = [];

  function recurse(node: Node, sink: OutlineNode[]) {
    const rule = ruleByType.get(node.type);
    let target = sink;
    if (rule) {
      const item: OutlineNode = {
        kind: rule.kind,
        name: findName(node, rule),
        startLine: node.startPosition.row + 1,
        endLine: node.endPosition.row + 1,
        children: [],
      };
      sink.push(item);
      target = item.children;
    }
    for (let i = 0; i < node.childCount; i++) {
      const c = node.child(i);
      if (c) recurse(c, target);
    }
  }
  recurse(root, out);
  return out;
}

function render(items: OutlineNode[], depth = 0): string {
  const lines: string[] = [];
  for (const it of items) {
    const indent = "  ".repeat(depth);
    lines.push(
      `${indent}${it.kind} ${it.name} [${it.startLine}-${it.endLine}]`,
    );
    if (it.children.length > 0) {
      lines.push(render(it.children, depth + 1));
    }
  }
  return lines.filter(Boolean).join("\n");
}

async function outlineFile(path: string): Promise<string> {
  const ext = extname(path);
  const config = LANGS[ext];
  if (!config) {
    return `# ${path}\n  (unsupported extension: ${ext})`;
  }
  const lang = await Language.load(readFileSync(config.wasm));
  const parser = new Parser();
  parser.setLanguage(lang);
  const src = readFileSync(path, "utf8");
  const tree = parser.parse(src);
  if (!tree) return `# ${path}\n  (parse failed)`;
  const items = buildOutline(tree.rootNode, config.rules);
  return `# ${path}\n${render(items)}`;
}

// Directories we never recurse into when walking a tree.
const SKIP_DIRS = new Set([
  ".git",
  "node_modules",
  ".venv",
  "venv",
  "__pycache__",
  "target",
  "dist",
  "build",
  ".tmp",
  "result",
]);

function* walk(root: string): Generator<string> {
  const stack: string[] = [root];
  while (stack.length > 0) {
    const dir = stack.pop()!;
    let entries: ReturnType<typeof readdirSync>;
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const entry of entries) {
      if (entry.name.startsWith(".") && entry.name !== ".") continue;
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (SKIP_DIRS.has(entry.name)) continue;
        stack.push(full);
      } else if (entry.isFile()) {
        if (LANGS[extname(entry.name)]) yield full;
      }
    }
  }
}

const target = process.argv[2];
if (!target) {
  console.error("usage: bun run outline.ts <file-or-dir>");
  process.exit(2);
}

await Parser.init();

const stat = statSync(target);
if (stat.isDirectory()) {
  const files = [...walk(target)].sort();
  for (const f of files) {
    console.log(await outlineFile(f));
    console.log("");
  }
} else {
  console.log(await outlineFile(target));
}
