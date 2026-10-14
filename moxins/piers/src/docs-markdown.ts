import { spawnSync } from "child_process";
import { readFile } from "fs/promises";
import { dirname, isAbsolute, resolve } from "path";
import { PiersError, gwsBin, gwsJson } from "./gws";
import {
  ImageHost,
  type PreparedImage,
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
// order.
async function hostImages(plan: Plan, host: ImageHost): Promise<Plan> {
  const out: Plan = [];
  for (const request of plan) {
    if (!request.images) {
      out.push(request);
      continue;
    }
    const images: PreparedImage[] = request.images;
    for (const image of [...images].reverse()) {
      out.push({
        insertInlineImage: {
          location: request.location,
          uri: await host.host(image),
          objectSize: {
            width: { magnitude: image.widthPt, unit: "PT" },
            height: { magnitude: image.heightPt, unit: "PT" },
          },
        },
      });
    }
  }
  return out;
}

// Every location and range names the tab (and segment, for a footnote);
// every index moves by `delta`.
function placed(
  plan: Plan,
  where: { tabId?: string; segmentId?: string; delta?: number },
): Plan {
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
  return plan.map((request) => walk(request));
}

// Sends one batchUpdate. Docs fetches a just-shared image only after a
// moment, so "problem retrieving the image" on a hosted image is retried; on
// a remote image (its URL is the markdown's own) the image becomes a visible
// "[image: url]" and a warning, and the batch is sent again.
async function sendBatch(
  gws: string,
  workDir: string,
  documentId: string,
  plan: Plan,
  host: ImageHost,
  revisionId: string | undefined,
  warnings: string[],
): Promise<any> {
  const requests = [...plan];
  // Fetch failures so far, per hosted image: each gets its own retries.
  const failures = new Map<string, number>();
  for (;;) {
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
      const { location, uri } = request.insertInlineImage;
      if (host.hosts(uri)) {
        const failed = (failures.get(uri) ?? 0) + 1;
        if (failed === hostedFetchAttempts) throw error;
        failures.set(uri, failed);
        await new Promise((wake) => setTimeout(wake, failed * 1500));
      } else {
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
  const batch: Plan = [];
  for (const [i, footnoteId] of footnoteIds.entries()) {
    const segment = segments[footnoteId];
    if (!segment) {
      throw new PiersError(
        `footnote ${footnoteId} created but not found in tab ${tab.tabProperties.tabId}`,
      );
    }
    const insertAt = segment.content.at(-1).endIndex - 1;
    batch.push(
      ...placed(await hostImages(footnotes[i], host), {
        tabId: tab.tabProperties.tabId,
        segmentId: footnoteId,
        delta: insertAt - 1,
      }),
    );
  }
  await sendBatch(gws, workDir, documentId, batch, host, undefined, warnings);
}

// Creates a Doc from markdown, or rewrites one tab of an existing Doc
// (default: its first) leaving every other tab alone. Returns the tool
// result: the new file's metadata, or the document and tab written, plus any
// warnings.
export async function writeMarkdown(
  workDir: string,
  source: MarkdownSource,
  target: WriteTarget,
): Promise<Record<string, unknown>> {
  const gws = gwsBin;
  const warnings: string[] = [];
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
    const batch = placed([...clear, ...(await hostImages(body, host))], {
      tabId,
    });
    const reply = await sendBatch(
      gws,
      workDir,
      documentId,
      batch,
      host,
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
  return result;
}
