import { spawnSync } from "child_process";
import { randomBytes } from "crypto";
import { readFile, writeFile } from "fs/promises";
import { dirname, extname, isAbsolute, join, resolve } from "path";
import { $ } from "zx";

// Google's markdown→Doc import cannot be trusted with diagrams: a local image
// path survives as a broken placeholder, SVG is never rendered, PlantUML
// source stays a code block, and every image it does embed is fixed at
// 0.75pt per pixel — 96 dpi, with no way to say otherwise (#449).
//
// So create/update take images out of the markdown before upload, leaving a
// placeholder token where each one stood, and afterwards put them in with the
// Docs API's insertInlineImage, which accepts an explicit display size and so
// lets a high-resolution render be shown at its natural size. That request
// fetches the image by URL, anonymously: each image is uploaded to Drive,
// shared by link, inserted, and deleted again. Docs keeps its own copy.

// Diagrams are rendered at twice their natural resolution (PlantUML's natural
// resolution is 96 dpi) and displayed at natural size.
const renderScale = 2;
const naturalDpi = 96;
const pointsPerPixel = 72 / naturalDpi;

// The text area of the default page: 8.5in x 11in with 1in margins, less a
// little height so an image shares its page with a line of text.
const maxDisplayWidthPt = 468;
const maxDisplayHeightPt = 620;

// insertInlineImage rejects images above 25 megapixels; stay well under.
const maxRenderedPixels = 16_000_000;

const rasterMimeTypeByExtension: Record<string, string> = {
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
};
const plantumlExtensions = [".puml", ".plantuml", ".pu"];
const supportedExtensions = [
  ...Object.keys(rasterMimeTypeByExtension),
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

type PixelSize = { width: number; height: number };

// An image taken out of the markdown, to be placed where `token` now stands.
export type PendingImage = {
  token: string;
  bytes: Buffer;
  mimeType: string;
  widthPt: number;
  heightPt: number;
};

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

function pngSize(png: Buffer): PixelSize {
  // 8-byte signature, then the IHDR chunk: length, "IHDR", width, height.
  if (png.length < 24 || png.toString("latin1", 12, 16) !== "IHDR") {
    throw new ImageError("not a PNG");
  }
  return { width: png.readUInt32BE(16), height: png.readUInt32BE(20) };
}

function gifSize(gif: Buffer): PixelSize {
  if (gif.length < 10 || gif.toString("latin1", 0, 3) !== "GIF") {
    throw new ImageError("not a GIF");
  }
  return { width: gif.readUInt16LE(6), height: gif.readUInt16LE(8) };
}

function jpegSize(jpeg: Buffer): PixelSize {
  // Walk the marker segments to the first start-of-frame (SOF0..SOF15, minus
  // the three codes in that range that are not frames).
  let offset = 2;
  while (offset + 9 <= jpeg.length && jpeg[offset] === 0xff) {
    const marker = jpeg[offset + 1];
    const isStartOfFrame =
      marker >= 0xc0 && marker <= 0xcf && ![0xc4, 0xc8, 0xcc].includes(marker);
    if (isStartOfFrame) {
      return {
        height: jpeg.readUInt16BE(offset + 5),
        width: jpeg.readUInt16BE(offset + 7),
      };
    }
    offset += 2 + jpeg.readUInt16BE(offset + 2);
  }
  throw new ImageError("not a JPEG with a readable frame header");
}

const rasterSizeByMimeType: Record<string, (bytes: Buffer) => PixelSize> = {
  "image/png": pngSize,
  "image/jpeg": jpegSize,
  "image/gif": gifSize,
};

// How large to show an image whose natural size is `natural` pixels: at
// 0.75pt per pixel, shrunk as needed to fit the page's text area.
function displaySizePt(natural: PixelSize): {
  widthPt: number;
  heightPt: number;
} {
  const widthPt = natural.width * pointsPerPixel;
  const heightPt = natural.height * pointsPerPixel;
  const fit = Math.min(
    1,
    maxDisplayWidthPt / widthPt,
    maxDisplayHeightPt / heightPt,
  );
  const roundPt = (points: number) => Math.round(points * fit * 1000) / 1000;
  return { widthPt: roundPt(widthPt), heightPt: roundPt(heightPt) };
}

// Renders at `renderScale` times natural resolution, or at whatever lower
// scale keeps the result under the megapixel cap. `render(scale)` returns a
// PNG; the natural size is recovered from the first render.
function renderSharp(render: (scale: number) => Buffer): {
  png: Buffer;
  natural: PixelSize;
} {
  const png = render(renderScale);
  const rendered = pngSize(png);
  const natural = {
    width: rendered.width / renderScale,
    height: rendered.height / renderScale,
  };
  const pixels = rendered.width * rendered.height;
  if (pixels <= maxRenderedPixels) return { png, natural };
  const cappedScale = renderScale * Math.sqrt(maxRenderedPixels / pixels);
  return { png: render(cappedScale), natural };
}

function renderPlantuml(source: string, cwd: string | undefined) {
  const diagram = /^\s*@start/m.test(source)
    ? source
    : `@startuml\n${source}\n@enduml\n`;
  return renderSharp((scale) =>
    runRenderer(
      "plantuml",
      [
        "-tpng",
        "-pipe",
        "-headless",
        `-Sdpi=${Math.floor(naturalDpi * scale)}`,
      ],
      { input: diagram, cwd },
    ),
  );
}

function renderSvg(path: string) {
  return renderSharp((scale) =>
    runRenderer(
      "rsvg-convert",
      ["--format", "png", "--zoom", scale.toFixed(3), path],
      {},
    ),
  );
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

function closesFence(line: string, opener: string): boolean {
  const closer = line.match(/^ {0,3}(`{3,}|~{3,})\s*$/)?.[1];
  return (
    closer !== undefined &&
    closer[0] === opener[0] &&
    closer.length >= opener.length
  );
}

// Collects the images taken out of one markdown document. Tokens are plain
// alphanumerics so the importer passes them through as ordinary text, and
// carry a per-run nonce so they cannot collide with the document's own words.
class ImageExtraction {
  readonly images: PendingImage[] = [];
  private readonly nonce = randomBytes(4).toString("hex").toUpperCase();

  constructor(private readonly baseDir: string | undefined) {}

  private add(bytes: Buffer, mimeType: string, natural: PixelSize): string {
    const token = `PIERSIMG${this.nonce}N${this.images.length + 1}E`;
    this.images.push({ token, bytes, mimeType, ...displaySizePt(natural) });
    return token;
  }

  addPlantuml(source: string, cwd: string | undefined): string {
    const { png, natural } = renderPlantuml(source, cwd);
    return this.add(png, "image/png", natural);
  }

  private async addLocalFile(
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
    let bytes: Buffer;
    try {
      bytes = await readFile(path);
    } catch (error) {
      throw new ImageError(
        `image ${destination}: cannot read ${path}: ${(error as Error).message}`,
      );
    }
    try {
      if (extension === ".svg") {
        const { png, natural } = renderSvg(path);
        return this.add(png, "image/png", natural);
      }
      if (plantumlExtensions.includes(extension)) {
        return this.addPlantuml(bytes.toString("utf8"), dirname(path));
      }
      const mimeType = rasterMimeTypeByExtension[extension];
      return this.add(bytes, mimeType, rasterSizeByMimeType[mimeType](bytes));
    } catch (error) {
      if (!(error instanceof ImageError)) throw error;
      throw new ImageError(`image ${destination}: ${error.message}`);
    }
  }

  async extractFromLine(line: string): Promise<string> {
    if (!this.baseDir) return line;
    let rewritten = "";
    let cursor = 0;
    for (const match of line.matchAll(imageReference)) {
      const [whole, , destination] = match;
      if (uriScheme.test(destination) || isInsideCodeSpan(line, match.index)) {
        continue;
      }
      const token = await this.addLocalFile(destination, this.baseDir);
      rewritten += line.slice(cursor, match.index) + token;
      cursor = match.index + whole.length;
    }
    return rewritten + line.slice(cursor);
  }
}

// Takes the images piers places itself out of `markdown`, returning the
// markdown with a token where each stood, and the images:
//
//   - a fenced code block whose language is `plantuml` (or `puml`) is rendered;
//   - when `baseDir` is given, every ![alt](path) without a URI scheme is read
//     relative to it: PNG/JPEG/GIF as-is, SVG rasterized,
//     .puml/.plantuml/.pu rendered.
//
// References that already carry a URI scheme (https:, data:, …) are left for
// Google's importer; anything inside other fenced code blocks or code spans is
// left untouched. An image that cannot be read or rendered throws: a silently
// broken diagram is worse than no Doc.
export async function extractImages(
  markdown: string,
  baseDir: string | undefined,
): Promise<{ markdown: string; images: PendingImage[] }> {
  const extraction = new ImageExtraction(baseDir);
  const lines = markdown.split("\n");
  const rewritten: string[] = [];
  for (let i = 0; i < lines.length; i++) {
    const [, fence, language = ""] = lines[i].match(fenceOpener) ?? [];
    if (!fence) {
      rewritten.push(await extraction.extractFromLine(lines[i]));
      continue;
    }
    let end = i + 1;
    while (end < lines.length && !closesFence(lines[end], fence)) end++;
    const isClosed = end < lines.length;
    if (isClosed && plantumlFenceLanguages.includes(language.toLowerCase())) {
      const source = lines.slice(i + 1, end).join("\n");
      rewritten.push(extraction.addPlantuml(source, baseDir));
    } else {
      rewritten.push(...lines.slice(i, end + 1));
    }
    i = end;
  }
  return { markdown: rewritten.join("\n"), images: extraction.images };
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

// The markdown a create/update entrypoint uploads — the caller's `content`,
// or the file at `contentPath` — with its images taken out. Local images
// resolve against `baseDir`, which defaults to the directory of
// `contentPath`. Absent arguments arrive as empty strings.
export async function markdownForUpload(
  content: string | undefined,
  baseDir: string | undefined,
  contentPath: string | undefined,
): Promise<{ markdown: string; images: PendingImage[] }> {
  const markdown = await readMarkdownSource(content, contentPath);
  const imageDir =
    baseDir || (contentPath ? dirname(resolve(contentPath)) : undefined);
  return extractImages(markdown, imageDir);
}

// Runs gws from `cwd` and parses its JSON reply. gws reports API errors on
// stdout, so that is what a failure carries.
export async function gwsJson(
  gws: string,
  cwd: string,
  args: string[],
): Promise<any> {
  const result = await $({
    cwd,
    nothrow: true,
    verbose: false,
    stdio: ["pipe", "pipe", "ignore"],
  })`${gws} ${args}`;
  if (result.exitCode !== 0) {
    throw new ImageError(
      `gws ${args.slice(0, 3).join(" ")} failed: ${result.stdout.trim()}`,
    );
  }
  const stdout = result.stdout.trim();
  return stdout ? JSON.parse(stdout) : {};
}

// The document index of every token found in `content` (a Docs API
// StructuralElement list), descending into tables.
function collectTokenIndexes(
  content: any[],
  tokens: string[],
  found: Map<string, number>,
): void {
  for (const element of content ?? []) {
    for (const run of element.paragraph?.elements ?? []) {
      const text: string = run.textRun?.content ?? "";
      for (const token of tokens) {
        const offset = text.indexOf(token);
        if (offset >= 0) found.set(token, (run.startIndex ?? 0) + offset);
      }
    }
    for (const row of element.table?.tableRows ?? []) {
      for (const cell of row.tableCells ?? []) {
        collectTokenIndexes(cell.content, tokens, found);
      }
    }
  }
}

const imageExtensionByMimeType: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
};

// Uploads one image to Drive and shares it by link, so the Docs API can fetch
// it. Returns the Drive file id; the caller deletes it.
async function hostTransiently(
  gws: string,
  workDir: string,
  image: PendingImage,
): Promise<string> {
  const fileName = `${image.token}.${imageExtensionByMimeType[image.mimeType]}`;
  await writeFile(join(workDir, fileName), image.bytes);
  const file = await gwsJson(gws, workDir, [
    "drive",
    "files",
    "create",
    "--json",
    JSON.stringify({ name: `piers-transient-${fileName}` }),
    "--upload",
    fileName,
    "--upload-content-type",
    image.mimeType,
  ]);
  await gwsJson(gws, workDir, [
    "drive",
    "permissions",
    "create",
    "--params",
    JSON.stringify({ fileId: file.id }),
    "--json",
    JSON.stringify({ role: "reader", type: "anyone" }),
  ]);
  return file.id;
}

const imageNotYetFetchable = "problem retrieving the image";

// Sends the batchUpdate, retrying while Docs cannot yet fetch a file whose
// link-sharing was granted a moment ago.
async function batchUpdateOnceFetchable(
  gws: string,
  workDir: string,
  documentId: string,
  requests: unknown[],
): Promise<void> {
  const args = [
    "docs",
    "documents",
    "batchUpdate",
    "--params",
    JSON.stringify({ documentId }),
    "--json",
    JSON.stringify({ requests }),
  ];
  for (let attempt = 1; ; attempt++) {
    try {
      await gwsJson(gws, workDir, args);
      return;
    } catch (error) {
      const retryable =
        error instanceof ImageError &&
        error.message.includes(imageNotYetFetchable);
      if (!retryable || attempt === 4) throw error;
      await new Promise((wake) => setTimeout(wake, attempt * 1500));
    }
  }
}

// Replaces each image's token in the Doc with the image, at its display size.
// The images are public-by-link on Drive for the duration of this call and
// deleted before it returns, whether or not it succeeds.
export async function placeImages(
  gws: string,
  workDir: string,
  documentId: string,
  images: PendingImage[],
): Promise<void> {
  if (images.length === 0) return;
  const hostedFileIds: string[] = [];
  try {
    const document = await gwsJson(gws, workDir, [
      "docs",
      "documents",
      "get",
      "--params",
      JSON.stringify({ documentId, fields: "body" }),
    ]);
    const indexByToken = new Map<string, number>();
    collectTokenIndexes(
      document.body?.content,
      images.map((image) => image.token),
      indexByToken,
    );

    const placements: { index: number; requests: unknown[] }[] = [];
    for (const image of images) {
      const index = indexByToken.get(image.token);
      if (index === undefined) {
        throw new ImageError(
          `placeholder ${image.token} not found in document ${documentId}`,
        );
      }
      const fileId = await hostTransiently(gws, workDir, image);
      hostedFileIds.push(fileId);
      placements.push({
        index,
        requests: [
          {
            deleteContentRange: {
              range: {
                startIndex: index,
                endIndex: index + image.token.length,
              },
            },
          },
          {
            insertInlineImage: {
              location: { index },
              uri: `https://drive.google.com/uc?export=download&id=${fileId}`,
              objectSize: {
                width: { magnitude: image.widthPt, unit: "PT" },
                height: { magnitude: image.heightPt, unit: "PT" },
              },
            },
          },
        ],
      });
    }
    // Last placeholder first, so earlier indexes stay valid as text changes.
    placements.sort((a, b) => b.index - a.index);
    await batchUpdateOnceFetchable(
      gws,
      workDir,
      documentId,
      placements.flatMap((placement) => placement.requests),
    );
  } finally {
    for (const fileId of hostedFileIds) {
      try {
        await gwsJson(gws, workDir, [
          "drive",
          "files",
          "delete",
          "--params",
          JSON.stringify({ fileId }),
        ]);
      } catch (error) {
        process.stderr.write(
          `WARNING: Drive file ${fileId} is still shared by link and could not be deleted: ${(error as Error).message}\n`,
        );
      }
    }
  }
}

// Shared body of create and update: upload the markdown with `uploadArgs`
// (which create the Doc or replace an existing one's body), then place its
// images. Prints the upload's reply as the tool result. Any ImageError —
// a bad argument, an unrenderable image, a failed gws call — becomes a
// one-line stderr message and exit 1.
export async function uploadMarkdownWithImages(
  workDir: string,
  source: {
    content: string | undefined;
    baseDir: string | undefined;
    contentPath: string | undefined;
  },
  uploadArgs: string[],
): Promise<void> {
  // GWS_BIN is the test seam: the wrapper pins PATH, so a stub cannot shadow
  // gws there.
  const gws = process.env.GWS_BIN ?? "gws";
  try {
    const { markdown, images } = await markdownForUpload(
      source.content,
      source.baseDir,
      source.contentPath,
    );
    await writeFile(join(workDir, "upload.md"), markdown);
    // gws rejects an --upload path outside its working directory, so it runs
    // from the work dir and is handed the bare file name.
    const file = await gwsJson(gws, workDir, [
      ...uploadArgs,
      "--upload",
      "upload.md",
      "--upload-content-type",
      "text/markdown",
    ]);
    try {
      await placeImages(gws, workDir, file.id, images);
    } catch (error) {
      if (!(error instanceof ImageError)) throw error;
      throw new ImageError(
        `document ${file.id} was written, but its images were not placed (placeholders remain): ${error.message}`,
      );
    }
    process.stdout.write(
      JSON.stringify({
        content: [
          {
            type: "text",
            text: JSON.stringify(file, null, 2),
            mimeType: "application/json",
          },
        ],
      }),
    );
  } catch (error) {
    if (!(error instanceof ImageError)) throw error;
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}
