import { $ } from "zx";

$.verbose = false;

const anchorName = process.argv[2];
if (!anchorName) {
  process.stderr.write("usage: anchor <anchor-name>\n");
  process.exit(1);
}

const markdown = await Bun.stdin.text();
if (!markdown.trim()) {
  process.stderr.write("no markdown input on stdin\n");
  process.exit(1);
}

// gfm reader keeps a Header following an inline `<a name="X">` as a real
// Header block. The default markdown reader collapses them into a single
// Para, which makes the output unparseable for our purposes. See moxy#186.
const { stdout } = await $({ input: markdown })`pandoc -f gfm -t json`;
const ast = JSON.parse(stdout);

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function inlinesOf(block: any): any[] {
  if (!block || !block.c) return [];
  switch (block.t) {
    case "Para":
    case "Plain":
      return Array.isArray(block.c) ? block.c : [];
    case "Header":
      return Array.isArray(block.c) && Array.isArray(block.c[2])
        ? block.c[2]
        : [];
    default:
      return [];
  }
}

function blockHasNamedAnchor(block: any, name: string): boolean {
  const re = new RegExp(
    `<a\\s+(?:name|id)\\s*=\\s*["']${escapeRegex(name)}["']`,
  );
  for (const i of inlinesOf(block)) {
    if (
      i.t === "RawInline" &&
      Array.isArray(i.c) &&
      i.c[0] === "html" &&
      re.test(i.c[1] as string)
    ) {
      return true;
    }
  }
  return false;
}

const ANY_ANCHOR_RE = /<a\s+(?:name|id)\s*=/;
function blockHasAnyAnchor(block: any): boolean {
  for (const i of inlinesOf(block)) {
    if (
      i.t === "RawInline" &&
      Array.isArray(i.c) &&
      i.c[0] === "html" &&
      ANY_ANCHOR_RE.test(i.c[1] as string)
    ) {
      return true;
    }
  }
  return false;
}

const { blocks } = ast;
const startIdx = blocks.findIndex((b: any) =>
  blockHasNamedAnchor(b, anchorName),
);

if (startIdx === -1) {
  process.stderr.write(`Anchor "${anchorName}" not found.\n`);
  process.exit(1);
}

let endIdx = blocks.length;
for (let i = startIdx + 1; i < blocks.length; i++) {
  if (blockHasAnyAnchor(blocks[i])) {
    endIdx = i;
    break;
  }
}

const sliced = { ...ast, blocks: blocks.slice(startIdx, endIdx) };
// gfm writer preserves raw HTML anchors verbatim instead of pandoc's
// `<tag>`{=html} escape syntax — matches the gfm reader for clean round-trip.
const result = await $({
  input: JSON.stringify(sliced),
})`pandoc -f json -t gfm --wrap=none`;
process.stdout.write(result.stdout);
