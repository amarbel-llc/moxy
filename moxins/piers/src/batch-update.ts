import { PiersError, gwsBin, gwsJson, printResult, runTool } from "./gws";

const [documentId, requests, revisionId] = process.argv.slice(2);

await runTool(async (workDir) => {
  let parsed: unknown;
  try {
    parsed = JSON.parse(requests);
  } catch (error) {
    throw new PiersError(`requests is not JSON: ${(error as Error).message}`);
  }
  printResult(
    await gwsJson(gwsBin, workDir, [
      "docs",
      "documents",
      "batchUpdate",
      "--params",
      JSON.stringify({ documentId }),
      "--json",
      JSON.stringify({
        requests: parsed,
        writeControl: { requiredRevisionId: revisionId },
      }),
    ]),
  );
});
