import { spawnSync } from "child_process";
import { readFile } from "fs/promises";
import { dirname, isAbsolute, resolve } from "path";
import {
  ImageHost,
  PiersError,
  type PreparedImage,
  gwsJson,
  prepareLocalImage,
  renderPlantuml,
} from "./images";

// piers create/update write markdown into a Doc through the Docs API, not
// Drive's markdown import: pandoc parses the markdown and a custom writer
// (docs-requests.lua) turns it into batchUpdate requests. The import cannot
// render code blocks (#457), only ever replaces a whole file, every tab with
// it (#456), and needed a regex scanner of piers' own to get images in
// (#453). See zz-pocs/pandoc-docs-poc for how the requests are built.

// The writer's store path, baked in at build time (flake.nix).
const docsWriter = "@PIERS_DOCS_WRITER@";

type Request = Record<string, any>;
type Plan = Request[];
type Pending =
  { kind: "file"; path: string } | { kind: "plantuml"; source: string };

// What a caller asked to write.
export type MarkdownSource = {
  content: string | undefined;
  baseDir: string | undefined;
  contentPath: string | undefined;
};

export type WriteTarget =
  | { kind: "create"; title: string }
  | { kind: "update"; fileId: string; tabId: string | undefined };

const imageNotFetchable =
  /requests\[(\d+)\]\.insertInlineImage: There was a problem retrieving the image/;
const hostedFetchAttempts = 4;

async function readMarkdownSource(source: MarkdownSource): Promise<string> {
  if (source.content && source.contentPath) {
    throw new PiersError("pass either content or content_path, not both");
  }
  if (source.contentPath) {
    try {
      return await readFile(source.contentPath, "utf8");
    } catch (error) {
      throw new PiersError(
        `content_path: cannot read ${source.contentPath}: ${(error as Error).message}`,
      );
    }
  }
  if (!source.content) {
    throw new PiersError("one of content or content_path is required");
  }
  return source.content;
}

// The writer's output: the body's requests, and one plan per footnote in the
// order their createFootnote requests appear.
function renderMarkdown(markdown: string): { body: Plan; footnotes: Plan[] } {
  const result = spawnSync(
    "pandoc",
    ["--preserve-tabs", "-f", "gfm", "-t", docsWriter],
    { input: markdown, maxBuffer: 256 * 1024 * 1024 },
  );
  if (result.error) throw new PiersError(`pandoc: ${result.error.message}`);
  if (result.status !== 0) {
    throw new PiersError(
      `pandoc exited ${result.status}: ${result.stderr
        .toString()
        .trim()
        .replace(/\s*\n\s*/g, "; ")}`,
    );
  }
  const output = JSON.parse(result.stdout.toString("utf8"));
  // An empty Lua table encodes as {}, not [].
  const list = (value: unknown) => (Array.isArray(value) ? value : []);
  return {
    body: list(output.requests),
    footnotes: list(output.footnotes).map((plan: any) => list(plan.requests)),
  };
}

function decodeDestination(destination: string): string {
  try {
    return decodeURIComponent(destination);
  } catch {
    return destination;
  }
}

// Each image the writer left pending is read and rendered now, so a bad
// image fails the call before anything is written. A local image with no
// directory to resolve it against becomes a visible "[image: path]" and a
// warning.
async function prepareImages(
  plan: Plan,
  imageDir: string | undefined,
  warnings: string[],
): Promise<Plan> {
  const out: Plan = [];
  for (const request of plan) {
    const pending: Pending | undefined = request.pending;
    if (!pending) {
      out.push(request);
      continue;
    }
    const location = request.insertInlineImage.location;
    if (pending.kind === "plantuml") {
      out.push({ location, images: renderPlantuml(pending.source, imageDir) });
    } else if (imageDir) {
      const decoded = decodeDestination(pending.path);
      const path = isAbsolute(decoded) ? decoded : resolve(imageDir, decoded);
      out.push({
        location,
        images: await prepareLocalImage(path, pending.path),
      });
    } else {
      warnings.push(
        `local image ${pending.path} was not placed: pass base_dir or content_path so it can be found`,
      );
      out.push({ insertText: { location, text: `[image: ${pending.path}]` } });
    }
  }
  return out;
}

// Hosts each prepared image and turns it into insertInlineImage requests at
// its location — in reverse, so several diagrams from one fence end up in
// order. Returns the requests and the set of those fetching a hosted image.
async function hostImages(
  plan: Plan,
  host: ImageHost,
): Promise<{ plan: Plan; hosted: Set<Request> }> {
  const out: Plan = [];
  const hosted = new Set<Request>();
  for (const request of plan) {
    if (!request.images) {
      out.push(request);
      continue;
    }
    const images: PreparedImage[] = request.images;
    for (const image of [...images].reverse()) {
      const placed = {
        insertInlineImage: {
          location: request.location,
          uri: await host.host(image),
          objectSize: {
            width: { magnitude: image.widthPt, unit: "PT" },
            height: { magnitude: image.heightPt, unit: "PT" },
          },
        },
      };
      hosted.add(placed);
      out.push(placed);
    }
  }
  return { plan: out, hosted };
}

// Every location and range names the tab (and segment, for a footnote);
// every index moves by `delta`. Request objects that are keys of `hosted`
// are mapped to their placed copies there.
function placed(
  plan: Plan,
  where: { tabId?: string; segmentId?: string; delta?: number },
  hosted: Set<Request> = new Set(),
): { plan: Plan; hosted: Set<Request> } {
  const delta = where.delta ?? 0;
  const walk = (value: any, key?: string): any => {
    if (Array.isArray(value)) return value.map((v) => walk(v));
    if (value === null || typeof value !== "object") {
      return ["index", "startIndex", "endIndex"].includes(key ?? "") &&
        typeof value === "number"
        ? value + delta
        : value;
    }
    const out: Record<string, any> = {};
    for (const [k, v] of Object.entries(value)) out[k] = walk(v, k);
    if (key === "location" || key === "range" || key === "tableStartLocation") {
      if (where.tabId) out.tabId = where.tabId;
      if (where.segmentId) out.segmentId = where.segmentId;
    }
    return out;
  };
  const placedHosted = new Set<Request>();
  const out = plan.map((request) => {
    const copy = walk(request);
    if (hosted.has(request)) placedHosted.add(copy);
    return copy;
  });
  return { plan: out, hosted: placedHosted };
}

// Sends one batchUpdate. Docs fetches a just-shared image only after a
// moment, so "problem retrieving the image" on a hosted image is retried; on
// a remote image (its URL is the markdown's own) the image becomes a visible
// "[image: url]" and a warning, and the batch is sent again.
async function sendBatch(
  gws: string,
  workDir: string,
  documentId: string,
  batch: { plan: Plan; hosted: Set<Request> },
  revisionId: string | undefined,
  warnings: string[],
): Promise<any> {
  const requests = [...batch.plan];
  for (let hostedAttempt = 1; ;) {
    const body: Record<string, unknown> = { requests };
    if (revisionId) body.writeControl = { requiredRevisionId: revisionId };
    try {
      return await gwsJson(gws, workDir, [
        "docs",
        "documents",
        "batchUpdate",
        "--params",
        JSON.stringify({ documentId }),
        "--json",
        JSON.stringify(body),
      ]);
    } catch (error) {
      const failing =
        error instanceof PiersError
          ? error.message.match(imageNotFetchable)
          : null;
      if (!failing) throw error;
      const index = Number(failing[1]);
      const request = requests[index];
      if (batch.hosted.has(request)) {
        if (hostedAttempt === hostedFetchAttempts) throw error;
        await new Promise((wake) => setTimeout(wake, hostedAttempt * 1500));
        hostedAttempt++;
      } else {
        const { location, uri } = request.insertInlineImage;
        warnings.push(
          `remote image ${uri} could not be fetched by Google Docs and was not placed`,
        );
        requests[index] = { insertText: { location, text: `[image: ${uri}]` } };
      }
    }
  }
}

const getDocument = (gws: string, workDir: string, documentId: string) =>
  gwsJson(gws, workDir, [
    "docs",
    "documents",
    "get",
    "--params",
    JSON.stringify({ documentId, includeTabsContent: true }),
  ]);

function allTabs(tabs: any[]): any[] {
  return (tabs ?? []).flatMap((tab) => [tab, ...allTabs(tab.childTabs)]);
}

function findTab(document: any, tabId: string | undefined): any {
  const tabs = allTabs(document.tabs);
  const tab = tabId
    ? tabs.find((t) => t.tabProperties.tabId === tabId)
    : tabs[0];
  if (!tab) {
    const known = tabs
      .map((t) => `${t.tabProperties.tabId} (${t.tabProperties.title})`)
      .join(", ");
    throw new PiersError(
      `document ${document.documentId} has no tab ${tabId}; its tabs: ${known}`,
    );
  }
  return tab;
}

// Writes the footnotes' content: each plan goes before its segment's final
// newline (a fresh footnote holds " \n", Docs' leading space).
async function writeFootnotes(
  gws: string,
  workDir: string,
  documentId: string,
  tabId: string | undefined,
  footnoteIds: string[],
  footnotes: Plan[],
  host: ImageHost,
  warnings: string[],
): Promise<void> {
  if (footnoteIds.length === 0) return;
  if (footnoteIds.length !== footnotes.length) {
    throw new PiersError(
      `created ${footnoteIds.length} footnotes, expected ${footnotes.length}`,
    );
  }
  const tab = findTab(await getDocument(gws, workDir, documentId), tabId);
  const segments = tab.documentTab.footnotes ?? {};
  const batch = { plan: [] as Plan, hosted: new Set<Request>() };
  for (const [i, footnoteId] of footnoteIds.entries()) {
    const insertAt = segments[footnoteId].content.at(-1).endIndex - 1;
    const hosted = await hostImages(footnotes[i], host);
    const one = placed(
      hosted.plan,
      {
        tabId: tab.tabProperties.tabId,
        segmentId: footnoteId,
        delta: insertAt - 1,
      },
      hosted.hosted,
    );
    batch.plan.push(...one.plan);
    for (const r of one.hosted) batch.hosted.add(r);
  }
  await sendBatch(gws, workDir, documentId, batch, undefined, warnings);
}

// Creates a Doc from markdown, or rewrites one tab of an existing Doc
// (default: its first) leaving every other tab alone. Prints the tool
// result; any PiersError becomes a one-line stderr message and exit 1.
export async function writeMarkdown(
  workDir: string,
  source: MarkdownSource,
  target: WriteTarget,
): Promise<void> {
  // GWS_BIN is the test seam: the wrapper pins PATH, so a stub cannot shadow
  // gws there.
  const gws = process.env.GWS_BIN ?? "gws";
  const warnings: string[] = [];
  try {
    const markdown = await readMarkdownSource(source);
    const imageDir =
      source.baseDir ||
      (source.contentPath ? dirname(resolve(source.contentPath)) : undefined);
    const rendered = renderMarkdown(markdown);
    const body = await prepareImages(rendered.body, imageDir, warnings);
    const footnotes: Plan[] = [];
    for (const plan of rendered.footnotes)
      footnotes.push(await prepareImages(plan, imageDir, warnings));

    let documentId: string;
    let tabId: string | undefined;
    let revisionId: string | undefined;
    let result: Record<string, unknown>;
    const clear: Plan = [];
    if (target.kind === "create") {
      const file = await gwsJson(gws, workDir, [
        "drive",
        "files",
        "create",
        "--json",
        JSON.stringify({
          name: target.title,
          mimeType: "application/vnd.google-apps.document",
        }),
      ]);
      documentId = file.id;
      result = file;
    } else {
      documentId = target.fileId;
      const document = await getDocument(gws, workDir, documentId);
      const tab = findTab(document, target.tabId);
      tabId = tab.tabProperties.tabId;
      // Batch 1 fails rather than overwrite edits made since this read.
      revisionId = document.revisionId;
      // Everything but the body's final newline, which cannot be deleted.
      const end = tab.documentTab.body.content.at(-1).endIndex;
      if (end - 1 > 1) {
        clear.push({
          deleteContentRange: { range: { startIndex: 1, endIndex: end - 1 } },
        });
      }
      result = { id: documentId, tabId };
    }

    const host = new ImageHost(gws, workDir);
    let writingError: unknown;
    try {
      const hosted = await hostImages(body, host);
      const batch = placed(
        [...clear, ...hosted.plan],
        { tabId },
        hosted.hosted,
      );
      const reply = await sendBatch(
        gws,
        workDir,
        documentId,
        batch,
        revisionId,
        warnings,
      );
      const footnoteIds: string[] = (reply.replies ?? [])
        .filter((r: any) => r?.createFootnote)
        .map((r: any) => r.createFootnote.footnoteId);
      await writeFootnotes(
        gws,
        workDir,
        documentId,
        tabId,
        footnoteIds,
        footnotes,
        host,
        warnings,
      );
    } catch (error) {
      writingError = error;
    }
    const undeleted = await host.deleteAll();
    if (undeleted.length > 0) {
      const writing =
        writingError instanceof Error ? `${writingError.message}; ` : "";
      throw new PiersError(
        `${writing}Drive file(s) ${undeleted.join(", ")} are still shared by link and could not be deleted: delete them by hand`,
      );
    }
    if (writingError) {
      if (!(writingError instanceof PiersError)) throw writingError;
      throw new PiersError(
        `writing document ${documentId} did not complete: ${writingError.message}`,
      );
    }
    if (warnings.length > 0) result.warnings = warnings;
    process.stdout.write(
      JSON.stringify({
        content: [
          {
            type: "text",
            text: JSON.stringify(result, null, 2),
            mimeType: "application/json",
          },
        ],
      }),
    );
  } catch (error) {
    if (!(error instanceof PiersError)) throw error;
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}
