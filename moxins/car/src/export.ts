import { $ } from "zx";
import { mkdtemp, readFile, rm } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [fileId, mimeType] = process.argv.slice(2);

const mime = mimeType || "text/markdown";
const dir = await mkdtemp(join(tmpdir(), "gws-export-"));
const tmp = join(dir, "export");

try {
  const params = JSON.stringify({ fileId, mimeType: mime });
  await $`gws drive files export --params ${params} --output ${tmp}`;
  const content = await readFile(tmp, "utf-8");

  process.stdout.write(
    JSON.stringify({
      content: [{ type: "text", text: content, mimeType: "text/plain" }],
    }),
  );
} finally {
  await rm(dir, { recursive: true, force: true });
}
