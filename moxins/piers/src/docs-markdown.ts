import { spawnSync } from "child_process";
import { readFile } from "fs/promises";
import { dirname, isAbsolute, resolve } from "path";
import { PiersError, docsBatchUpdate, gwsJson } from "./gws";
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
type Reply = Record<string, any>;
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

// gws's --json flag takes the whole batchUpdate body as one argv string,
// which Linux caps per-string at MAX_ARG_STRLEN (128 KiB) regardless of how
// the rest of the command line is built (#471: a few hundred plain
// paragraphs is enough). gws has no file/stdin form for --json to fall back
// to (`gws docs documents batchUpdate --help` lists only the literal-value
// flag); a plan over budget is split across several batchUpdate calls
// instead. The budget stays well under 128 KiB to leave room for the rest
// of the body (writeControl) and argv quoting overhead. PIERS_MAX_BATCH_BYTES
// is a test seam to exercise chunking with a small fixture.
const MAX_BATCH_BYTES = Number(process.env.PIERS_MAX_BATCH_BYTES) || 96 * 1024;

// docs-requests.lua emits a plan's entire body as one insertText request
// (all paragraphs concatenated, styled separately afterward) — see its
// header comment, step 1. That single request can itself be larger than
// MAX_BATCH_BYTES, which chunkPlan's request-granularity grouping cannot
// help with. Docs applies insertText requests in order, each pushing
// whatever follows forward by its own length, so a long insertText can be
// cut into several smaller ones at sequentially advancing indices with the
// same net effect as the one bulk insert — every other request's indices
// are absolute positions in the final text and stay correct either way.
// Splits by halving until each piece's own serialized size fits, so this is
// correct regardless of how much JSON-escaping a piece's text needs; never
// cuts a UTF-16 surrogate pair (Docs indices are UTF-16 code units, like
// `text.length`).
function splitInsertText(request: Request, budget: number): Plan {
  const { location, text } = request.insertText as {
    location: Record<string, unknown> & { index: number };
    text: string;
  };
  const parts: Plan = [];
  let offset = 0;
  let index = location.index;
  while (offset < text.length) {
    let length = Math.max(1, Math.min(text.length - offset, budget));
    for (;;) {
      let end = offset + length;
      if (end < text.length) {
        const code = text.charCodeAt(end - 1);
        if (code >= 0xd800 && code <= 0xdbff) end -= 1;
      }
      const slice = text.slice(offset, end);
      const piece = {
        insertText: { location: { ...location, index }, text: slice },
      };
      if (
        end - offset <= 1 ||
        Buffer.byteLength(JSON.stringify(piece), "utf8") <= budget
      ) {
        parts.push(piece);
        index += slice.length;
        offset = end;
        break;
      }
      length = Math.max(1, Math.floor((end - offset) / 2));
    }
  }
  return parts;
}

// Expands any request whose own serialized JSON is already over budget.
// Only an insertText's text is ours to split (#471 review follow-up): it is
// the one request shape docs-requests.lua emits with unbounded size, and
// splitting its literal text preserves every other request's indices (see
// splitInsertText). A different oversized request is passed through as-is
// — chunkPlan still gives it its own chunk, but that chunk can still be
// over budget; nothing else in the writer's output grows unboundedly with
// document length the way the body text does.
function expandOversizedRequests(plan: Plan, budget: number): Plan {
  const out: Plan = [];
  for (const request of plan) {
    const size = Buffer.byteLength(JSON.stringify(request), "utf8");
    if (size > budget && typeof request.insertText?.text === "string") {
      out.push(...splitInsertText(request, budget));
    } else {
      out.push(request);
    }
  }
  return out;
}

// Splits `plan` in order into groups whose serialized JSON stays under
// MAX_BATCH_BYTES. Never reorders requests, so a request that must run
// first (the tab-clearing deleteContentRange) stays first. Always returns
// at least one (possibly empty) chunk, matching a single batchUpdate call
// for an empty plan.
function chunkPlan(plan: Plan): Plan[] {
  const chunks: Plan[] = [];
  let current: Plan = [];
  let currentBytes = 0;
  for (const request of expandOversizedRequests(plan, MAX_BATCH_BYTES)) {
    const size = Buffer.byteLength(JSON.stringify(request), "utf8");
    if (current.length > 0 && currentBytes + size > MAX_BATCH_BYTES) {
      chunks.push(current);
      current = [];
      currentBytes = 0;
    }
    current.push(request);
    currentBytes += size;
  }
  if (current.length > 0 || chunks.length === 0) chunks.push(current);
  return chunks;
}

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

// Sends one chunk of a batchUpdate. Docs fetches a just-shared image only
// after a moment, so "problem retrieving the image" on a hosted image is
// retried; on a remote image (its URL is the markdown's own) the image
// becomes a visible "[image: url]" and a warning, and the chunk is sent
// again.
async function sendChunk(
  workDir: string,
  documentId: string,
  chunk: Plan,
  host: ImageHost,
  revisionId: string | undefined,
  warnings: string[],
): Promise<any> {
  const requests = [...chunk];
  // Fetch failures so far, per hosted image: each gets its own retries.
  const failures = new Map<string, number>();
  for (;;) {
    const body: Record<string, unknown> = { requests };
    if (revisionId) body.writeControl = { requiredRevisionId: revisionId };
    try {
      return await docsBatchUpdate(workDir, documentId, body);
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

// Sends `plan` as one or more batchUpdate calls (#471: gws's --json argv
// string is capped well below what a large plan's JSON can reach). Only the
// first chunk carries the revision check: it guards the read this plan was
// built from, and every later chunk is applied after our own first chunk, not
// before it. Splitting trades away the API's whole-request atomicity for
// plans big enough to span chunks — a failure partway through a multi-chunk
// plan leaves the earlier chunks applied, where a single-call plan is still
// all-or-nothing (linenisgreat/moxy#472 tracks this). A failure in a later
// chunk says so, so the caller can tell "nothing changed" from "partially
// rewritten" instead of seeing the same message either way.
async function sendBatch(
  workDir: string,
  documentId: string,
  plan: Plan,
  host: ImageHost,
  revisionId: string | undefined,
  warnings: string[],
): Promise<{ replies: Reply[] }> {
  const chunks = chunkPlan(plan);
  const replies: Reply[] = [];
  for (const [chunkIndex, chunk] of chunks.entries()) {
    let reply: { replies?: Reply[] };
    try {
      reply = await sendChunk(
        workDir,
        documentId,
        chunk,
        host,
        chunkIndex === 0 ? revisionId : undefined,
        warnings,
      );
    } catch (error) {
      if (chunkIndex > 0 && error instanceof PiersError) {
        throw new PiersError(
          `chunk ${chunkIndex + 1}/${chunks.length} failed, after ${chunkIndex} earlier chunk(s) already applied: ${error.message}`,
        );
      }
      throw error;
    }
    replies.push(...(reply.replies ?? []));
  }
  return { replies };
}

const getDocument = (workDir: string, documentId: string) =>
  gwsJson(workDir, [
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
  const tab = findTab(await getDocument(workDir, documentId), tabId);
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
  await sendBatch(workDir, documentId, batch, host, undefined, warnings);
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
    const file = await gwsJson(workDir, [
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
    const document = await getDocument(workDir, documentId);
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

  const host = new ImageHost(workDir);
  let writingError: unknown;
  try {
    const batch = placed([...clear, ...(await hostImages(body, host))], {
      tabId,
    });
    const reply = await sendBatch(
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
  if (writingError) {
    const undeleted = await host.deleteAll();
    if (undeleted.length > 0) {
      const writing =
        writingError instanceof Error ? `${writingError.message}; ` : "";
      throw new PiersError(
        `${writing}Drive file(s) ${undeleted.join(", ")} are still shared by link and could not be deleted: delete them by hand`,
      );
    }
    if (!(writingError instanceof PiersError)) throw writingError;
    throw new PiersError(
      `writing document ${documentId} did not complete: ${writingError.message}`,
    );
  }
  const images = host.keepAll();
  if (images.length > 0) result.images = images;
  if (warnings.length > 0) result.warnings = warnings;
  return result;
}
