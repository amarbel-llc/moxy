import { spawnSync } from "child_process";
import { readFile, writeFile } from "fs/promises";
import { dirname, extname, join } from "path";
import { PiersError, gwsBin, gwsJson } from "./gws";

// Images that the Docs API's insertInlineImage places: local files and
// PlantUML diagrams, rendered and sized here, then hosted on Drive for the
// moment Docs fetches them (#449).
//
// insertInlineImage takes an explicit display size, which lets a
// high-resolution render be shown at its natural size, and fetches the image
// by URL, anonymously: each image is uploaded to Drive, shared by link,
// inserted, and deleted again. Docs keeps its own copy.

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

type PixelSize = { width: number; height: number };

// An image ready to host: its bytes and the size to show it at.
export type PreparedImage = {
  bytes: Buffer;
  mimeType: string;
  widthPt: number;
  heightPt: number;
};

function runRenderer(
  command: string,
  args: string[],
  options: { input?: string; cwd?: string; env?: NodeJS.ProcessEnv },
): Buffer {
  const result = spawnSync(command, args, {
    ...options,
    maxBuffer: 256 * 1024 * 1024,
  });
  if (result.error) {
    throw new PiersError(`${command}: ${result.error.message}`);
  }
  if (result.status !== 0) {
    const stderr = result.stderr
      .toString()
      .trim()
      .replace(/\s*\n\s*/g, "; ");
    throw new PiersError(`${command} exited ${result.status}: ${stderr}`);
  }
  return result.stdout;
}

// The markdown piers publishes is often not the caller's own (a cloned
// repo's README, a pasted document), and what PlantUML draws ends up in a
// shared Doc. Under its default profile `!include /some/file` would draw that
// file's text and `!include https://…` would fetch from this host; SANDBOX
// turns file and network access off. The size limit is raised from PlantUML's
// 4096px default, which would silently crop a 2x render of a tall diagram;
// renderSharp's megapixel cap does the limiting instead.
const plantumlEnv = {
  ...process.env,
  PLANTUML_SECURITY_PROFILE: "SANDBOX",
  PLANTUML_LIMIT_SIZE: "16384",
};

function pngSize(png: Buffer): PixelSize {
  // 8-byte signature, then the IHDR chunk: length, "IHDR", width, height.
  if (png.length < 24 || png.toString("latin1", 12, 16) !== "IHDR") {
    throw new PiersError("not a PNG");
  }
  return { width: png.readUInt32BE(16), height: png.readUInt32BE(20) };
}

function gifSize(gif: Buffer): PixelSize {
  if (gif.length < 10 || gif.toString("latin1", 0, 3) !== "GIF") {
    throw new PiersError("not a GIF");
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
  throw new PiersError("not a JPEG with a readable frame header");
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

function prepared(
  bytes: Buffer,
  mimeType: string,
  natural: PixelSize,
): PreparedImage {
  if (!(natural.width > 0 && natural.height > 0)) {
    throw new PiersError("image has no width or height");
  }
  return { bytes, mimeType, ...displaySizePt(natural) };
}

// Renders at `renderScale` times natural resolution, or at whatever lower
// scale keeps the result under the megapixel cap. `render(scale)` returns a
// PNG; the natural size is recovered from the first render.
function renderSharp(render: (scale: number) => Buffer): PreparedImage {
  const png = render(renderScale);
  const rendered = pngSize(png);
  const natural = {
    width: rendered.width / renderScale,
    height: rendered.height / renderScale,
  };
  const pixels = rendered.width * rendered.height;
  if (pixels <= maxRenderedPixels) return prepared(png, "image/png", natural);
  const cappedScale = renderScale * Math.sqrt(maxRenderedPixels / pixels);
  return prepared(render(cappedScale), "image/png", natural);
}

// The diagrams in one PlantUML source: each @startX … @endX block, or the
// whole source wrapped in @startuml/@enduml when it has none. Rendered one
// by one, since `-pipe` concatenates the PNGs of several diagrams (#453).
function plantumlDiagrams(source: string): string[] {
  const blocks = source.match(/^\s*@start(\w+)[\s\S]*?^\s*@end\1\b.*$/gm);
  return blocks ?? [`@startuml\n${source}\n@enduml\n`];
}

export function renderPlantuml(
  source: string,
  cwd: string | undefined,
): PreparedImage[] {
  return plantumlDiagrams(source).map((diagram) =>
    renderSharp((scale) =>
      runRenderer(
        "plantuml",
        [
          "-tpng",
          "-pipe",
          "-headless",
          `-Sdpi=${Math.floor(naturalDpi * scale)}`,
        ],
        { input: diagram, cwd, env: plantumlEnv },
      ),
    ),
  );
}

function renderSvg(path: string): PreparedImage {
  return renderSharp((scale) =>
    runRenderer(
      "rsvg-convert",
      ["--format", "png", "--zoom", scale.toFixed(3), path],
      {},
    ),
  );
}

// Reads and renders the local image at `path`: PNG/JPEG/GIF as-is, SVG
// rasterized, .puml/.plantuml/.pu rendered (one image per diagram).
export async function prepareLocalImage(
  path: string,
  label: string,
): Promise<PreparedImage[]> {
  const extension = extname(path).toLowerCase();
  if (!supportedExtensions.includes(extension)) {
    throw new PiersError(
      `image ${label}: unsupported extension (supported: ${supportedExtensions.join(", ")})`,
    );
  }
  let bytes: Buffer;
  try {
    bytes = await readFile(path);
  } catch (error) {
    throw new PiersError(
      `image ${label}: cannot read ${path}: ${(error as Error).message}`,
    );
  }
  try {
    if (extension === ".svg") return [renderSvg(path)];
    if (plantumlExtensions.includes(extension)) {
      return renderPlantuml(bytes.toString("utf8"), dirname(path));
    }
    const mimeType = rasterMimeTypeByExtension[extension];
    return [prepared(bytes, mimeType, rasterSizeByMimeType[mimeType](bytes))];
  } catch (error) {
    if (!(error instanceof PiersError)) throw error;
    throw new PiersError(`image ${label}: ${error.message}`);
  }
}

const imageExtensionByMimeType: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
};

function deleteHostedFileArgs(fileId: string): string[] {
  return ["drive", "files", "delete", "--params", JSON.stringify({ fileId })];
}

const terminationSignals = ["SIGTERM", "SIGINT", "SIGHUP"] as const;

// Images uploaded to Drive and shared by link for as long as Docs needs to
// fetch them. The owner must call `deleteAll` whatever happens; a termination
// signal (a client cancel, moxy's async timeout) deletes them too, since
// nothing else knows their ids.
export class ImageHost {
  private readonly fileIds: string[] = [];
  private readonly uris = new Set<string>();
  private uploads = 0;
  private readonly onSignal = () => {
    for (const fileId of this.fileIds) {
      spawnSync(gwsBin, deleteHostedFileArgs(fileId), {
        cwd: this.workDir,
        stdio: "ignore",
      });
    }
    process.exit(1);
  };

  constructor(private readonly workDir: string) {
    for (const signal of terminationSignals) process.on(signal, this.onSignal);
  }

  // Uploads one image and shares it by link; returns the URL Docs fetches.
  // The id is recorded the moment the file exists — before it is shared — so
  // the file is deleted even when sharing fails. One at a time: concurrent
  // permission grants were answered with a 500.
  async host(image: PreparedImage): Promise<string> {
    const fileName = `image-${++this.uploads}.${imageExtensionByMimeType[image.mimeType]}`;
    await writeFile(join(this.workDir, fileName), image.bytes);
    const file = await gwsJson(this.workDir, [
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
    this.fileIds.push(file.id);
    await gwsJson(this.workDir, [
      "drive",
      "permissions",
      "create",
      "--params",
      JSON.stringify({ fileId: file.id }),
      "--json",
      JSON.stringify({ role: "reader", type: "anyone" }),
    ]);
    const uri = `https://drive.google.com/uc?export=download&id=${file.id}`;
    this.uris.add(uri);
    return uri;
  }

  // Whether `uri` is one this host put up (as opposed to the markdown's own).
  hosts(uri: string): boolean {
    return this.uris.has(uri);
  }

  // Deletes every hosted file, trying each twice, and stops watching for
  // signals. Returns the ids that are still there.
  async deleteAll(): Promise<string[]> {
    const undeleted: string[] = [];
    for (const fileId of this.fileIds) {
      try {
        await gwsJson(this.workDir, deleteHostedFileArgs(fileId));
      } catch {
        try {
          await gwsJson(this.workDir, deleteHostedFileArgs(fileId));
        } catch {
          undeleted.push(fileId);
        }
      }
    }
    for (const signal of terminationSignals) process.off(signal, this.onSignal);
    return undeleted;
  }
}
