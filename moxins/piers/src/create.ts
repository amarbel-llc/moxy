import { mkdtemp, rm } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import { writeMarkdown } from "./docs-markdown";

const [title, content, baseDir, contentPath] = process.argv.slice(2);

// gws runs from here: uploads must sit under its cwd, and it drops a
// download.<ext> there for any non-JSON reply (#458).
const workDir = await mkdtemp(join(tmpdir(), "piers-"));

try {
  await writeMarkdown(
    workDir,
    { content, baseDir, contentPath },
    { kind: "create", title },
  );
} finally {
  await rm(workDir, { recursive: true, force: true });
}
