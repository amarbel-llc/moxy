import { readFile } from "fs/promises";
import { join } from "path";
import { gwsText, printText, runGwsTool } from "../../../moxin-lib/gws-run";

const [fileId, mimeType] = process.argv.slice(2);

const mime = mimeType || "text/markdown";
const outputName = "export";

await runGwsTool(async (workDir) => {
  const params = JSON.stringify({ fileId, mimeType: mime });
  // gws refuses an --output path outside its cwd (#447), so the scratch dir is
  // its cwd and the name is relative to it.
  await gwsText(workDir, [
    "drive",
    "files",
    "export",
    "--params",
    params,
    "--output",
    outputName,
  ]);
  printText(await readFile(join(workDir, outputName), "utf-8"), "text/plain");
});
