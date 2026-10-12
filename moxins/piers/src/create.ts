import { $ } from "zx";
import { mkdtemp, writeFile, rm } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import { markdownForUpload } from "./inline-images";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [title, content, baseDir, contentPath] = process.argv.slice(2);

// GWS_BIN is the test seam: the wrapper pins PATH, so a stub cannot shadow gws.
const gws = process.env.GWS_BIN ?? "gws";

const markdown = await markdownForUpload(content, baseDir, contentPath);

const dir = await mkdtemp(join(tmpdir(), "gws-upload-"));
const tmp = join(dir, "upload.md");

try {
  await writeFile(tmp, markdown);
  const json = JSON.stringify({
    name: title,
    mimeType: "application/vnd.google-apps.document",
  });
  // gws rejects an --upload path outside its working directory, so it runs
  // from the temp dir and is handed the bare file name.
  const result = await $({
    cwd: dir,
  })`${gws} drive files create --json ${json} --upload upload.md --upload-content-type text/markdown`;

  process.stdout.write(
    JSON.stringify({
      content: [
        { type: "text", text: result.stdout, mimeType: "application/json" },
      ],
    }),
  );
} finally {
  await rm(dir, { recursive: true, force: true });
}
