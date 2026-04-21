import { $ } from "zx";

$.verbose = false;

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

const lines: string[] = [];
for (const block of ast.blocks) {
  if (block.t === "Header") {
    const [level, , inlines] = block.c;
    lines.push("#".repeat(level) + " " + inlineText(inlines));
  }
}

process.stdout.write(lines.join("\n") + "\n");
