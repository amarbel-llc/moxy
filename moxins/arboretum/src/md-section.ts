import { $ } from "zx";
import { readMarkdownStdin } from "./md-input.ts";

$.verbose = false;

const sectionName = process.argv[2];
if (!sectionName) {
  throw new Error("usage: section <heading-name>");
}

const markdown = await readMarkdownStdin();

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
  const available = blocks
    .filter((b: any) => b.t === "Header")
    .map((b: any) => {
      const [level, , inlines] = b.c;
      return "#".repeat(level) + " " + inlineText(inlines);
    })
    .join("\n");
  throw new Error(
    `Section "${sectionName}" not found. Available sections:\n${available}`,
  );
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
