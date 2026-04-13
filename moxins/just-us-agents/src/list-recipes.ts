import { $ } from "zx";
import { readdirSync, existsSync } from "fs";
import { join, dirname, relative } from "path";

$.verbose = false;

interface Recipe {
  name: string;
  doc: string | null;
  parameters: { name: string; kind: string; default: string | null }[];
  dependencies: { recipe: string }[];
}

function formatRecipes(
  recipes: Record<string, Recipe>,
  prefix: string,
): string[] {
  return Object.entries(recipes)
    .sort(([a], [b]) => a.localeCompare(b))
    .filter(([, r]) => !r.name.startsWith("_"))
    .map(([, r]) => {
      const params = r.parameters
        .map(
          (p) =>
            ` ${p.kind === "star" ? "*" : ""}${p.name}${p.default != null ? `=${p.default}` : ""}`,
        )
        .join("");
      const doc = r.doc ? `  # ${r.doc}` : "";
      const deps =
        r.dependencies.length > 0
          ? `  [deps: ${r.dependencies.map((d) => d.recipe).join(", ")}]`
          : "";
      return `${prefix}${r.name}${params}${doc}${deps}`;
    });
}

// Root justfile recipes
const rootDump = JSON.parse((await $`just --dump --dump-format json`).stdout);
const lines = formatRecipes(rootDump.recipes, "");

// Child justfile recipes from subdirectories
const findResult =
  await $`find . -mindepth 2 -maxdepth 3 -name justfile -not -path './.worktrees/*' -not -path './.git/*' -not -path './.claude/*'`.nothrow();

if (findResult.stdout.trim()) {
  const childPaths = findResult.stdout.trim().split("\n").sort();
  for (const child of childPaths) {
    const dir = relative(".", dirname(child));
    try {
      const childDump = JSON.parse(
        (
          await $`just --dump --dump-format json --justfile ${child} --working-directory ${dir}`
        ).stdout,
      );
      lines.push(...formatRecipes(childDump.recipes, `${dir}/`));
    } catch {
      // skip justfiles that fail to parse
    }
  }
}

const text = lines.join("\n");
const result = {
  content: [{ type: "text", text, mimeType: "text/plain" }],
};
process.stdout.write(JSON.stringify(result));
