import { PiersError, gwsJson, printResult, runTool } from "./gws";

// piers.rename: set a Google Doc's title (moxy#516). The title is a Drive file
// property, not part of the Docs API, so this is Drive files.update with only
// `name` in the body: the document's content, comments and anchors are not
// touched.

const [documentId, title] = process.argv.slice(2);

await runTool(async (workDir) => {
  if (!title || !title.trim()) {
    throw new PiersError("title must not be empty");
  }
  const updated = await gwsJson(workDir, [
    "drive",
    "files",
    "update",
    "--params",
    JSON.stringify({ fileId: documentId, fields: "id,name" }),
    "--json",
    JSON.stringify({ name: title }),
  ]);
  // The name as Drive reports it, not the one we sent.
  printResult({ id: updated.id ?? documentId, name: updated.name });
});
