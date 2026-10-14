import { gwsJson, printResult, runTool } from "./gws";

const [documentId, rawIncludeTabsContent] = process.argv.slice(2);

const params: Record<string, unknown> = { documentId };
if (rawIncludeTabsContent === "true") params.includeTabsContent = true;

await runTool(async (workDir) =>
  printResult(
    await gwsJson(workDir, [
      "docs",
      "documents",
      "get",
      "--params",
      JSON.stringify(params),
    ]),
  ),
);
