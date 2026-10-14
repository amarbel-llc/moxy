import { writeMarkdown } from "./docs-markdown";
import { printResult, runTool } from "./gws";

const [title, content, baseDir, contentPath] = process.argv.slice(2);

await runTool(async (workDir) =>
  printResult(
    await writeMarkdown(
      workDir,
      { content, baseDir, contentPath },
      { kind: "create", title },
    ),
  ),
);
