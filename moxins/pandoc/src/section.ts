import { $ } from "zx";

$.verbose = false;

const sectionName = process.argv[2];
if (!sectionName) {
  process.stderr.write("usage: section <heading-name>\n");
  process.exit(1);
}

const markdown = await Bun.stdin.text();
if (!markdown.trim()) {
  process.stderr.write("no markdown input on stdin\n");
  process.exit(1);
}

const { stdout } = await $({ input: markdown })`pandoc -f markdown -t json`;
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

const { blocks } = ast;
const target = sectionName.toLowerCase();

const startIdx = blocks.findIndex(
  (b: any) => b.t === "Header" && inlineText(b.c[2]).toLowerCase() === target,
);

if (startIdx === -1) {
  process.stderr.write(
    `Section "${sectionName}" not found. Available sections:\n`,
  );
  for (const b of blocks) {
    if (b.t === "Header") {
      const [level, , inlines] = b.c;
      process.stderr.write(
        "#".repeat(level) + " " + inlineText(inlines) + "\n",
      );
    }
  }
  process.exit(1);
}

const level = blocks[startIdx].c[0];
let endIdx = blocks.length;
for (let i = startIdx + 1; i < blocks.length; i++) {
  if (blocks[i].t === "Header" && blocks[i].c[0] <= level) {
    endIdx = i;
    break;
  }
}

const sliced = { ...ast, blocks: blocks.slice(startIdx, endIdx) };
const result = await $({
  input: JSON.stringify(sliced),
})`pandoc -f json -t markdown --wrap=none`;
process.stdout.write(result.stdout);
