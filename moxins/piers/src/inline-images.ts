import { spawnSync } from "child_process";
import { readFile } from "fs/promises";
import { dirname, extname, isAbsolute, resolve } from "path";

// Google's markdown→Doc import embeds an image only when it can fetch the
// bytes itself: an https URL or a data: URI. A local path survives as a
// broken, zero-sized placeholder, an SVG is never rendered, and PlantUML
// source stays a code block (#449). So before upload this module turns every
// local image, and every PlantUML diagram, into a PNG/JPEG/GIF data: URI. The
// bytes travel inside the import request: nothing is hosted anywhere and
// nothing outlives the document.

// The import sizes an image at 0.75pt per pixel and ignores every size hint
// (HTML width, PNG DPI metadata). 624px is the text width of the default page
// (8.5in minus two 1in margins, at 96px/in); anything wider overflows it, so
// images this module rasterizes itself are capped there.
const maxRenderedWidthPx = 624;
const plantumlDefaultDpi = 96;

const passthroughMimeTypeByExtension: Record<string, string> = {
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
};
const plantumlExtensions = [".puml", ".plantuml", ".pu"];
const supportedExtensions = [
  ...Object.keys(passthroughMimeTypeByExtension),
  ".svg",
  ...plantumlExtensions,
];
const plantumlFenceLanguages = ["plantuml", "puml"];

// ![alt](dest) and ![alt](dest "title"); dest may be <angle bracketed>.
const imageReference =
  /!\[([^\]]*)\]\(\s*(<[^>]*>|[^\s)]+)(\s+(?:"[^"]*"|'[^']*'))?\s*\)/g;

const uriScheme = /^[a-zA-Z][a-zA-Z0-9+.-]*:/;
const fenceOpener = /^ {0,3}(`{3,}|~{3,})\s*([^\s`]*)/;

export class ImageError extends Error {}

function runRenderer(
  command: string,
  args: string[],
  options: { input?: string; cwd?: string },
): Buffer {
  const result = spawnSync(command, args, {
    ...options,
    maxBuffer: 256 * 1024 * 1024,
  });
  if (result.error) {
    throw new ImageError(`${command}: ${result.error.message}`);
  }
  if (result.status !== 0) {
    throw new ImageError(
      `${command} exited ${result.status}: ${result.stderr.toString().trim()}`,
    );
  }
  return result.stdout;
}

function pngWidthPx(png: Buffer): number {
  // 8-byte signature, then the IHDR chunk: length, "IHDR", width, height.
  if (png.length < 24 || png.toString("latin1", 12, 16) !== "IHDR") {
    throw new ImageError("renderer did not produce a PNG");
  }
  return png.readUInt32BE(16);
}

function renderPlantuml(source: string, cwd: string | undefined): Buffer {
  const diagram = /^\s*@start/m.test(source)
    ? source
    : `@startuml\n${source}\n@enduml\n`;
  const render = (extraArgs: string[]) =>
    runRenderer("plantuml", ["-tpng", "-pipe", "-headless", ...extraArgs], {
      input: diagram,
      cwd,
    });
  const png = render([]);
  const width = pngWidthPx(png);
  if (width <= maxRenderedWidthPx) return png;
  const fittingDpi = Math.floor(
    (plantumlDefaultDpi * maxRenderedWidthPx) / width,
  );
  return render([`-Sdpi=${fittingDpi}`]);
}

function renderSvg(path: string): Buffer {
  const png = runRenderer("rsvg-convert", ["--format", "png", path], {});
  if (pngWidthPx(png) <= maxRenderedWidthPx) return png;
  return runRenderer(
    "rsvg-convert",
    [
      "--format",
      "png",
      "--width",
      String(maxRenderedWidthPx),
      "--keep-aspect-ratio",
      path,
    ],
    {},
  );
}

function dataUri(mimeType: string, bytes: Buffer): string {
  return `data:${mimeType};base64,${bytes.toString("base64")}`;
}

function isInsideCodeSpan(line: string, index: number): boolean {
  let backticks = 0;
  for (let i = 0; i < index; i++) if (line[i] === "`") backticks++;
  return backticks % 2 === 1;
}

function decodeDestination(destination: string): string {
  const unwrapped =
    destination.startsWith("<") && destination.endsWith(">")
      ? destination.slice(1, -1)
      : destination;
  try {
    return decodeURIComponent(unwrapped);
  } catch {
    return unwrapped;
  }
}

async function readLocalFile(
  destination: string,
  path: string,
): Promise<Buffer> {
  try {
    return await readFile(path);
  } catch (error) {
    throw new ImageError(
      `image ${destination}: cannot read ${path}: ${(error as Error).message}`,
    );
  }
}

async function dataUriForLocalImage(
  destination: string,
  baseDir: string,
): Promise<string> {
  const decoded = decodeDestination(destination);
  const path = isAbsolute(decoded) ? decoded : resolve(baseDir, decoded);
  const extension = extname(path).toLowerCase();
  if (!supportedExtensions.includes(extension)) {
    throw new ImageError(
      `image ${destination}: unsupported extension (supported: ${supportedExtensions.join(", ")})`,
    );
  }
  const bytes = await readLocalFile(destination, path);
  try {
    if (extension === ".svg") return dataUri("image/png", renderSvg(path));
    if (plantumlExtensions.includes(extension)) {
      return dataUri(
        "image/png",
        renderPlantuml(bytes.toString("utf8"), dirname(path)),
      );
    }
  } catch (error) {
    if (!(error instanceof ImageError)) throw error;
    throw new ImageError(`image ${destination}: ${error.message}`);
  }
  return dataUri(passthroughMimeTypeByExtension[extension], bytes);
}

async function inlineLocalImagesInLine(
  line: string,
  baseDir: string,
): Promise<string> {
  let rewritten = "";
  let cursor = 0;
  for (const match of line.matchAll(imageReference)) {
    const [whole, alt, destination, title = ""] = match;
    if (uriScheme.test(destination) || isInsideCodeSpan(line, match.index)) {
      continue;
    }
    const uri = await dataUriForLocalImage(destination, baseDir);
    rewritten += line.slice(cursor, match.index) + `![${alt}](${uri}${title})`;
    cursor = match.index + whole.length;
  }
  return rewritten + line.slice(cursor);
}

function closesFence(line: string, opener: string): boolean {
  const closer = line.match(/^ {0,3}(`{3,}|~{3,})\s*$/)?.[1];
  return (
    closer !== undefined &&
    closer[0] === opener[0] &&
    closer.length >= opener.length
  );
}

// Returns `markdown` with its images made embeddable by Google's importer:
//
//   - a fenced code block whose language is `plantuml` (or `puml`) is rendered
//     and replaced by the diagram;
//   - when `baseDir` is given, every ![alt](path) without a URI scheme is read
//     relative to it and embedded: PNG/JPEG/GIF as-is, SVG rasterized,
//     .puml/.plantuml/.pu rendered.
//
// References that already carry a URI scheme (https:, data:, …) and anything
// inside other fenced code blocks or code spans are left untouched. An image
// that cannot be read or rendered fails the whole call: a silently broken
// diagram is worse than no Doc.
export async function embedImages(
  markdown: string,
  baseDir: string | undefined,
): Promise<string> {
  const lines = markdown.split("\n");
  const embedded: string[] = [];
  for (let i = 0; i < lines.length; i++) {
    const [, fence, language = ""] = lines[i].match(fenceOpener) ?? [];
    if (!fence) {
      embedded.push(
        baseDir ? await inlineLocalImagesInLine(lines[i], baseDir) : lines[i],
      );
      continue;
    }
    let end = i + 1;
    while (end < lines.length && !closesFence(lines[end], fence)) end++;
    const isClosed = end < lines.length;
    if (isClosed && plantumlFenceLanguages.includes(language.toLowerCase())) {
      const png = renderPlantuml(lines.slice(i + 1, end).join("\n"), baseDir);
      embedded.push(`![PlantUML diagram](${dataUri("image/png", png)})`);
    } else {
      embedded.push(...lines.slice(i, end + 1));
    }
    i = end;
  }
  return embedded.join("\n");
}

async function readMarkdownSource(
  content: string | undefined,
  contentPath: string | undefined,
): Promise<string> {
  if (content && contentPath) {
    throw new ImageError("pass either content or content_path, not both");
  }
  if (contentPath) {
    try {
      return await readFile(contentPath, "utf8");
    } catch (error) {
      throw new ImageError(
        `content_path: cannot read ${contentPath}: ${(error as Error).message}`,
      );
    }
  }
  if (!content) {
    throw new ImageError("one of content or content_path is required");
  }
  return content;
}

// The markdown a create/update entrypoint uploads: the caller's `content`, or
// the file at `contentPath`, with its images embedded. Local images resolve
// against `baseDir`, which defaults to the directory of `contentPath`. A bad
// argument or image becomes a one-line stderr message and exit 1, before
// anything is uploaded. Absent arguments arrive as empty strings.
export async function markdownForUpload(
  content: string | undefined,
  baseDir: string | undefined,
  contentPath: string | undefined,
): Promise<string> {
  try {
    const markdown = await readMarkdownSource(content, contentPath);
    const imageDir =
      baseDir || (contentPath ? dirname(resolve(contentPath)) : undefined);
    return await embedImages(markdown, imageDir);
  } catch (error) {
    if (!(error instanceof ImageError)) throw error;
    process.stderr.write(`${error.message}\n`);
    process.exit(1);
  }
}
