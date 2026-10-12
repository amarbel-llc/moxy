import { mkdtemp, rm } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import { uploadMarkdownWithImages } from "./inline-images";

const [fileId, content, baseDir, contentPath] = process.argv.slice(2);

const workDir = await mkdtemp(join(tmpdir(), "gws-upload-"));

try {
  await uploadMarkdownWithImages(workDir, { content, baseDir, contentPath }, [
    "drive",
    "files",
    "update",
    "--params",
    JSON.stringify({ fileId }),
  ]);
} finally {
  await rm(workDir, { recursive: true, force: true });
}
