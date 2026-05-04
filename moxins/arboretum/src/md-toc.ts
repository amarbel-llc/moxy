import { $ } from "zx";

$.verbose = false;

const markdown = await Bun.stdin.text();
if (!markdown.trim()) {
  process.stderr.write("no markdown input on stdin\n");
  process.exit(1);
}

// gfm reader keeps headings adjacent to inline `<a name="X">` HTML anchors
// as real Header blocks; the default markdown reader collapses them into
// Paragraphs and the toc disappears. Same reader md-anchor.ts uses.
const { stdout } = await $({ input: markdown })`pandoc -f gfm -t json`;
const ast = JSON.parse(stdout);

function inlineText(inlines: any[]): string {
  return inlines
    .map((i: any) => {
      if (i.t === "Str") return i.c;
      if (i.t === "Space" || i.t === "SoftBreak") return " ";
      if (i.t === "LineBreak") return "\n";
      if (Array.isArray(i.c)) return inlineText(i.c);
      if (i.t === "Link" || i.t === "Image") return inlineText(i.c[1]);
      if (i.t === "Quoted") return inlineText(i.c[1]);
      return "";
    })
    .join("");
}

const lines: string[] = [];
for (const block of ast.blocks) {
  if (block.t === "Header") {
    const [level, attrs, inlines] = block.c;
    const id =
      Array.isArray(attrs) && typeof attrs[0] === "string" ? attrs[0] : "";
    const heading = "#".repeat(level) + " " + inlineText(inlines);
    lines.push(id ? `${heading}  #${id}` : heading);
  }
}

process.stdout.write(lines.join("\n") + "\n");
