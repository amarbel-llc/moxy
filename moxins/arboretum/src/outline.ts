import { Parser, Language, type Node } from "web-tree-sitter";
import { readFileSync, statSync, readdirSync } from "node:fs";
import { extname, join } from "node:path";

// @WASM_DIR@ is replaced at build time with the moxin's absolute
// share dir (e.g. /nix/store/...-arboretum-moxin/wasm). The placeholder
// pattern matches @BIN@ — both are substituted post-bundle by mkBunMoxin.
const WASM_DIR = "@WASM_DIR@";

type NodeRule = {
  type: string;
  kind: string;
  name: { field?: string; child?: string };
};

type LangConfig = {
  wasm: string;
  rules: NodeRule[];
};

const TS_RULES: NodeRule[] = [
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
];

const LANGS: Record<string, LangConfig> = {
  ".go": {
    wasm: `${WASM_DIR}/tree-sitter-go.wasm`,
    rules: [
      { type: "function_declaration", kind: "func", name: { field: "name" } },
      { type: "method_declaration", kind: "method", name: { field: "name" } },
      { type: "type_spec", kind: "type", name: { field: "name" } },
      { type: "const_spec", kind: "const", name: { child: "identifier" } },
      { type: "var_spec", kind: "var", name: { child: "identifier" } },
      { type: "field_declaration", kind: "field", name: { field: "name" } },
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
    rules: TS_RULES,
  },
  ".tsx": {
    wasm: `${WASM_DIR}/tree-sitter-tsx.wasm`,
    rules: TS_RULES,
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

const SKIP_DIRS = new Set([
  ".git", "node_modules", ".venv", "venv", "__pycache__",
  "target", "dist", "build", ".tmp", "result",
]);

function findName(node: Node, rule: NodeRule): string {
  if (rule.name.field) {
    const n = node.childForFieldName(rule.name.field);
    if (n) return n.text;
  }
  if (rule.name.child) {
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
  startLine: number;
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
    lines.push(`${indent}${it.kind} ${it.name} [${it.startLine}-${it.endLine}]`);
    if (it.children.length > 0) lines.push(render(it.children, depth + 1));
  }
  return lines.filter(Boolean).join("\n");
}

const langCache = new Map<string, { parser: Parser; rules: NodeRule[] }>();

async function getLang(ext: string): Promise<{ parser: Parser; rules: NodeRule[] } | null> {
  const config = LANGS[ext];
  if (!config) return null;
  let entry = langCache.get(ext);
  if (entry) return entry;
  const lang = await Language.load(readFileSync(config.wasm));
  const parser = new Parser();
  parser.setLanguage(lang);
  entry = { parser, rules: config.rules };
  langCache.set(ext, entry);
  return entry;
}

async function outlineFile(path: string): Promise<string> {
  const ext = extname(path);
  const got = await getLang(ext);
  if (!got) return `# ${path}\n  (unsupported extension: ${ext})`;
  const src = readFileSync(path, "utf8");
  const tree = got.parser.parse(src);
  if (!tree) return `# ${path}\n  (parse failed)`;
  const items = buildOutline(tree.rootNode, got.rules);
  const out = `# ${path}\n${render(items)}`;
  tree.delete();
  return out;
}

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
  console.error("usage: outline <file-or-dir>");
  process.exit(2);
}

await Parser.init();

try {
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
} catch (err) {
  console.error(`outline: ${target}: ${(err as Error).message}`);
  process.exit(2);
}
