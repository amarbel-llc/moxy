import { writeMarkdown } from "./docs-markdown";
import { printResult, runTool } from "./gws";

const [fileId, content, baseDir, contentPath, tabId] = process.argv.slice(2);

await runTool(async (workDir) =>
  printResult(
    await writeMarkdown(
      workDir,
      { content, baseDir, contentPath },
      { kind: "update", fileId, tabId: tabId || undefined },
    ),
  ),
);
