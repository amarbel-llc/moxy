import { $ } from "zx";
import { mkdtemp, writeFile, rm } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";

$.verbose = false;

const [fileId, content] = process.argv.slice(2);

const dir = await mkdtemp(join(tmpdir(), "gws-upload-"));
const tmp = join(dir, "upload.md");

try {
  await writeFile(tmp, content);
  const params = JSON.stringify({ fileId });
  const result = await $`gws drive files update --params ${params} --upload ${tmp} --upload-content-type text/markdown`;

  process.stdout.write(
    JSON.stringify({
      content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
    }),
  );
} finally {
  await rm(dir, { recursive: true, force: true });
}
