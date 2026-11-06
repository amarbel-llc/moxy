import { $ } from "zx";
import { mkdtemp, readFile, rm } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [fileId, mimeType] = process.argv.slice(2);

const mime = mimeType || "text/markdown";
const dir = await mkdtemp(join(tmpdir(), "gws-export-"));
const outputName = "export";

try {
  const params = JSON.stringify({ fileId, mimeType: mime });
  // gws refuses an --output path outside its cwd (#447), so it runs in the
  // scratch dir and writes a name relative to it.
  $.cwd = dir;
  await $`gws drive files export --params ${params} --output ${outputName}`;
  const content = await readFile(join(dir, outputName), "utf-8");

  process.stdout.write(
    JSON.stringify({
      content: [{ type: "text", text: content, mimeType: "text/plain" }],
    }),
  );
} finally {
  await rm(dir, { recursive: true, force: true });
}
