// POC phase 3: against the real Docs API. Creates a scratch Doc with old
// content in tab 1 and a sentinel second tab, clears tab 1's body, applies the
// writer's requests to tab 1 only (images hosted on Drive for the moment Docs
// fetches them; footnote content in a second batch once their ids are known),
// reads the Doc back and checks it. The Doc and hosted images are always
// deleted.
import { spawnSync } from "child_process";
import { mkdtemp, readFile, rm, writeFile } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import {
  FIXTURE,
  GWS,
  LOCAL_IMAGE,
  ONE_PIXEL_PNG_BASE64,
  writerOutput,
  fail as exitFailing,
} from "./common.mjs";

const TITLE = "piers pandoc-writer POC scratch (delete me)";
const SENTINEL = "SENTINEL: this tab must survive tab 1 being rewritten.";
const OLD_CONTENT =
  "Old heading\nOld body that must disappear.\n- old list item";
const FINAL_DOC_DUMP = join(tmpdir(), "pandoc-docs-poc-final-document.json");
const IMAGE_NOT_YET_FETCHABLE = "problem retrieving the image";
// true: leave the scratch Doc for a look in the Docs UI (delete it by hand).
const KEEP_DOC = false;

// Throws rather than exits, so the scratch Doc is still deleted.
const fail = (message) => {
  throw new Error(message);
};

// gws's cwd, and where the fixture's local image paths resolve.
const workDir = await mkdtemp(join(tmpdir(), "pandoc-docs-poc-"));

// gws refuses --upload paths outside its cwd, so it always runs in workDir.
async function gws(...args) {
  const result = await $({
    cwd: workDir,
    nothrow: true,
    verbose: false,
  })`${GWS} ${args}`;
  if (result.exitCode !== 0)
    throw new Error(
      `gws ${args.slice(0, 3).join(" ")}: ${result.stdout}${result.stderr}`,
    );
  return result.stdout.trim() ? JSON.parse(result.stdout) : {};
}

async function batchUpdate(documentId, requests) {
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
      return await gws(...args);
    } catch (error) {
      const failing = error.message.match(/requests\[(\d+)\]/)?.[1];
      if (failing !== undefined)
        console.log(
          `failing request ${failing}: ${JSON.stringify(requests[failing])}`,
        );
      if (!error.message.includes(IMAGE_NOT_YET_FETCHABLE) || attempt === 4)
        throw error;
      await new Promise((wake) => setTimeout(wake, attempt * 1500));
    }
  }
}
const getWithTabs = (documentId) =>
  gws(
    "docs",
    "documents",
    "get",
    "--params",
    JSON.stringify({ documentId, includeTabsContent: true }),
  );

// Every location and range names the tab (and segment, for a footnote);
// every index moves by `delta`.
function placed(requests, { tabId, segmentId, delta = 0 }) {
  const walk = (value, key) => {
    if (Array.isArray(value)) return value.map((v) => walk(v));
    if (!value || typeof value !== "object") {
      return ["index", "startIndex", "endIndex"].includes(key) &&
        typeof value === "number"
        ? value + delta
        : value;
    }
    const out = {};
    for (const [k, v] of Object.entries(value))
      out[k] = k === "pending" ? v : walk(v, k);
    if (key === "location" || key === "range" || key === "tableStartLocation") {
      out.tabId = tabId;
      if (segmentId) out.segmentId = segmentId;
    }
    return out;
  };
  return walk(requests);
}

const hostedFileIds = [];

async function hostPng(bytes, name) {
  await writeFile(join(workDir, name), bytes);
  const file = await gws(
    "drive",
    "files",
    "create",
    "--json",
    JSON.stringify({ name: `piers-poc-${name}` }),
    "--upload",
    name,
    "--upload-content-type",
    "image/png",
  );
  hostedFileIds.push(file.id);
  await gws(
    "drive",
    "permissions",
    "create",
    "--params",
    JSON.stringify({ fileId: file.id }),
    "--json",
    JSON.stringify({ role: "reader", type: "anyone" }),
  );
  return `https://drive.google.com/uc?export=download&id=${file.id}`;
}

async function renderPlantuml(source) {
  const diagram = /^\s*@start/m.test(source)
    ? source
    : `@startuml\n${source}\n@enduml\n`;
  // spawnSync keeps stdout a Buffer; zx decodes it as UTF-8, mangling the PNG.
  const result = spawnSync("plantuml", ["-tpng", "-pipe", "-headless"], {
    input: diagram,
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.status !== 0)
    throw new Error(`plantuml exited ${result.status}: ${result.stderr}`);
  return result.stdout;
}

// Fills in each pending image's uri and drops the `pending` field. One at a
// time: concurrent Drive permission grants were answered with a 500.
async function materialize(requests) {
  let n = 0;
  const out = [];
  for (const request of requests) out.push(await materializeOne(request, ++n));
  return out;
}

async function materializeOne(request, n) {
  {
    if (!request.pending) return request;
    const { pending, ...rest } = request;
    const name = `image-${n}.png`;
    const bytes =
      pending.kind === "plantuml"
        ? await renderPlantuml(pending.source)
        : await readFile(join(workDir, pending.path));
    const uri = await hostPng(bytes, name);
    console.log(
      `hosted ${name}: ${pending.kind} ${pending.path ?? ""} ${bytes.length} bytes, header ${bytes.subarray(0, 8).toString("hex")}`,
    );
    rest.insertInlineImage = { ...rest.insertInlineImage, uri };
    return rest;
  }
}

const paragraphsIn = (content) =>
  (content ?? []).filter((e) => e.paragraph).map((e) => e.paragraph);
const textOf = (paragraph) =>
  paragraph.elements.map((r) => r.textRun?.content ?? "").join("");
const tabById = (doc, tabId) =>
  doc.tabs.find((t) => t.tabProperties.tabId === tabId);

await writeFile(
  join(workDir, LOCAL_IMAGE),
  Buffer.from(ONE_PIXEL_PNG_BASE64, "base64"),
);
const file = await gws(
  "drive",
  "files",
  "create",
  "--json",
  JSON.stringify({
    name: TITLE,
    mimeType: "application/vnd.google-apps.document",
  }),
);
const documentId = file.id;
console.log(`scratch doc ${documentId}`);

let failure;
try {
  const initial = await getWithTabs(documentId);
  const firstTabId = initial.tabs[0].tabProperties.tabId;
  const added = await batchUpdate(documentId, [
    { addDocumentTab: { tabProperties: { title: "sentinel" } } },
  ]);
  const sentinelTabId = added.replies[0].addDocumentTab.tabProperties.tabId;
  await batchUpdate(documentId, [
    {
      insertText: {
        location: { index: 1, tabId: sentinelTabId },
        text: SENTINEL,
      },
    },
    {
      insertText: {
        location: { index: 1, tabId: firstTabId },
        text: OLD_CONTENT,
      },
    },
    {
      updateParagraphStyle: {
        range: { startIndex: 1, endIndex: 2, tabId: firstTabId },
        paragraphStyle: { namedStyleType: "HEADING_1" },
        fields: "namedStyleType",
      },
    },
  ]);

  // Batch 1: clear tab 1's body (all but its final newline), write the doc.
  const before = await getWithTabs(documentId);
  const bodyEnd = tabById(before, firstTabId).documentTab.body.content.at(
    -1,
  ).endIndex;
  const { requests, footnotes } = await writerOutput(FIXTURE);
  const batch1 = [
    {
      deleteContentRange: {
        range: { startIndex: 1, endIndex: bodyEnd - 1, tabId: firstTabId },
      },
    },
    ...placed(await materialize(requests), { tabId: firstTabId }),
  ];
  const reply1 = await batchUpdate(documentId, batch1);
  const footnoteIds = reply1.replies
    .filter((r) => r.createFootnote)
    .map((r) => r.createFootnote.footnoteId);
  if (footnoteIds.length !== footnotes.length)
    fail(
      `created ${footnoteIds.length} footnotes, writer planned ${footnotes.length}`,
    );

  // Batch 2: each footnote's content, at its segment's final newline.
  if (footnoteIds.length > 0) {
    const withFootnotes = await getWithTabs(documentId);
    const segments = tabById(withFootnotes, firstTabId).documentTab.footnotes;
    console.log(
      `fresh footnote segment: ${JSON.stringify(paragraphsIn(segments[footnoteIds[0]].content).map(textOf))}`,
    );
    const batch2 = [];
    for (const [i, footnoteId] of footnoteIds.entries()) {
      const insertAt = segments[footnoteId].content.at(-1).endIndex - 1;
      batch2.push(
        ...placed(await materialize(footnotes[i].requests), {
          tabId: firstTabId,
          segmentId: footnoteId,
          delta: insertAt - 1,
        }),
      );
    }
    await batchUpdate(documentId, batch2);
  }

  const after = await getWithTabs(documentId);
  await writeFile(FINAL_DOC_DUMP, JSON.stringify(after, null, 2));

  if (after.tabs.length !== 2)
    fail(`expected 2 tabs, got ${after.tabs.length}`);
  const sentinelTab = tabById(after, sentinelTabId);
  if (
    !sentinelTab ||
    !paragraphsIn(sentinelTab.documentTab.body.content).some((p) =>
      textOf(p).includes(SENTINEL),
    )
  ) {
    fail("sentinel tab lost or changed");
  }

  const tab = tabById(after, firstTabId).documentTab;
  const body = tab.body.content;
  const rewritten = paragraphsIn(body);
  if (rewritten.map(textOf).join("").includes("Old body"))
    fail("old tab-1 content survived the clear");

  const find = (needle) =>
    rewritten.find((p) => textOf(p).includes(needle)) ??
    fail(`no paragraph containing ${JSON.stringify(needle)}`);
  const runWith = (p, needle) =>
    p.elements.find((r) => r.textRun?.content.trimEnd() === needle) ??
    fail(`no run ${JSON.stringify(needle)} in ${JSON.stringify(textOf(p))}`);

  if (find("Heading one").paragraphStyle.namedStyleType !== "HEADING_1")
    fail("'Heading one' is not HEADING_1");
  if (find("Intro with").paragraphStyle.namedStyleType !== "NORMAL_TEXT")
    fail("intro inherited the cleared heading style");

  const code = find('echo "hello"');
  if (!code.paragraphStyle.shading?.backgroundColor?.color)
    fail("code paragraph is not shaded");
  if (
    code.elements[0].textRun.textStyle.weightedFontFamily?.fontFamily !==
    "Roboto Mono"
  )
    fail("code run is not monospace");
  if (!/indented line\u000b\twith a tab/.test(textOf(code)))
    fail(`code breaks/tab lost: ${JSON.stringify(textOf(code))}`);

  if (!find("item one").bullet) fail("'item one' has no bullet");
  const nested = find("nested");
  if (nested.bullet?.nestingLevel !== 1)
    fail(`nested item nestingLevel ${nested.bullet?.nestingLevel}`);
  if (textOf(nested).startsWith("\t")) fail("nested item kept its leading tab");
  if (!runWith(nested, "bold").textRun.textStyle.bold)
    fail("nested 'bold' lost its style");
  if (!runWith(find("after-emoji"), "after-emoji").textRun.textStyle.bold)
    fail("bold after a surrogate pair is misplaced");

  // Task list.
  const todo = find("todo item");
  if (!todo.bullet) fail("task item has no bullet");
  if (/[☐☒]/.test(textOf(todo))) fail("checkbox glyph left in the text");
  if (!runWith(find("done item"), "done item").textRun.textStyle.strikethrough)
    fail("done task not struck through");
  const taskLevel =
    tab.lists[todo.bullet.listId].listProperties.nestingLevels[0];
  console.log(
    `task list level 0: ${JSON.stringify({ glyphType: taskLevel.glyphType, glyphSymbol: taskLevel.glyphSymbol })}`,
  );

  // Ordered list continues across its interior code block.
  const listIds = ["first", "second", "third"].map(
    (t) => find(t).bullet?.listId,
  );
  if (!listIds[0] || new Set(listIds).size !== 1)
    fail(`ordered items are on lists ${JSON.stringify(listIds)}`);
  const interior = find("interior code");
  if (interior.bullet) fail("interior code block is bulleted");
  if (!interior.paragraphStyle.shading?.backgroundColor?.color)
    fail("interior code block lost its shading");

  // Table.
  const tableIndex = body.findIndex((e) => e.table);
  if (tableIndex < 0) fail("no table in tab 1");
  const table = body[tableIndex].table;
  const cells = table.tableRows.map((row) =>
    row.tableCells.map((cell) =>
      paragraphsIn(cell.content).map(textOf).join("").trimEnd(),
    ),
  );
  if (
    JSON.stringify(cells) !==
    JSON.stringify([
      ["Name", "Value"],
      ["alpha", "1"],
      ["beta", "2"],
    ])
  )
    fail(`table cells ${JSON.stringify(cells)}`);
  const cellParagraph = (r, c) =>
    paragraphsIn(table.tableRows[r].tableCells[c].content)[0];
  if (!cellParagraph(0, 0).elements[0].textRun.textStyle.bold)
    fail("header cell not bold");
  if (cellParagraph(1, 1).paragraphStyle.alignment !== "END")
    fail(
      `right-aligned column is ${cellParagraph(1, 1).paragraphStyle.alignment}`,
    );
  if (
    cellParagraph(1, 1).elements[0].textRun.textStyle.weightedFontFamily
      ?.fontFamily !== "Roboto Mono"
  )
    fail("code in cell not monospace");
  if (!table.tableRows[0].tableCells[0].tableCellStyle.backgroundColor?.color)
    fail("header row not shaded");
  // No empty paragraph either side: "third" ends right before the table, and
  // the paragraph after it is the next block, its own style intact even
  // though the deleted empty paragraph had been a list item's.
  const prev = body[tableIndex - 1]?.paragraph;
  const next = body[tableIndex + 1]?.paragraph;
  if (!prev || textOf(prev) !== "third\n")
    fail(`before the table: ${JSON.stringify(prev && textOf(prev))}`);
  if (!prev.bullet)
    fail("'third' lost its bullet when the table was inserted after it");
  if (!next || !textOf(next).startsWith("Text with a note"))
    fail(`after the table: ${JSON.stringify(next && textOf(next))}`);
  console.log(
    `paragraph after table: ${JSON.stringify({ style: next.paragraphStyle.namedStyleType, bullet: next.bullet ?? null, spaceBelow: next.paragraphStyle.spaceBelow, indentStart: next.paragraphStyle.indentStart })}`,
  );
  if (next.bullet)
    fail("paragraph after the table inherited the list item's bullet");
  if (next.paragraphStyle.namedStyleType !== "NORMAL_TEXT")
    fail(`paragraph after the table is ${next.paragraphStyle.namedStyleType}`);

  // Footnote.
  const noteParagraph = find("Text with a note");
  if (!noteParagraph.elements.some((e) => e.footnoteReference))
    fail("no footnote reference after 'Text with a note'");
  const footnoteSegments = Object.values(tab.footnotes ?? {});
  if (footnoteSegments.length !== 1)
    fail(`expected 1 footnote, got ${footnoteSegments.length}`);
  const noteText = paragraphsIn(footnoteSegments[0].content)
    .map(textOf)
    .join("");
  console.log(`footnote content: ${JSON.stringify(noteText)}`);
  if (!noteText.includes("A footnote with emph."))
    fail("footnote text missing");
  if (
    !paragraphsIn(footnoteSegments[0].content).some((p) =>
      p.elements.some(
        (r) => r.textRun?.content === "emph" && r.textRun.textStyle.italic,
      ),
    )
  ) {
    fail("footnote 'emph' not italic");
  }

  // Images: one in the stray-backtick line, two in the remote/local line,
  // one PlantUML diagram.
  const imagesIn = (p) =>
    p.elements.filter((e) => e.inlineObjectElement).length;
  if (imagesIn(find("A stray")) !== 1)
    fail("stray-backtick line image missing");
  const mixed = find("Remote");
  if (imagesIn(mixed) !== 2)
    fail(`remote/local line has ${imagesIn(mixed)} images`);
  if (!mixed.elements[0].textRun?.content.startsWith("Remote "))
    fail("first image placed before 'Remote '");
  const total = rewritten.reduce((n, p) => n + imagesIn(p), 0);
  if (total !== 4) fail(`expected 4 images in tab 1, got ${total}`);

  if (find("Final paragraph").bullet)
    fail("final paragraph inherited a bullet");

  console.log(
    `PASS: tab 1 rewritten (${rewritten.length} paragraphs, 1 table, 1 footnote, ${total} images), sentinel tab intact; final doc in ${FINAL_DOC_DUMP}`,
  );
} catch (error) {
  failure = error;
} finally {
  for (const fileId of hostedFileIds) {
    await gws(
      "drive",
      "files",
      "delete",
      "--params",
      JSON.stringify({ fileId }),
    ).catch((e) =>
      console.log(`could not delete hosted image ${fileId}: ${e.message}`),
    );
  }
  console.log(`deleted ${hostedFileIds.length} hosted image(s)`);
  if (KEEP_DOC) {
    console.log(
      `KEPT scratch doc: https://docs.google.com/document/d/${documentId}/edit`,
    );
  } else {
    await gws(
      "drive",
      "files",
      "delete",
      "--params",
      JSON.stringify({ fileId: documentId }),
    );
    console.log(`deleted scratch doc ${documentId}`);
  }
  await rm(workDir, { recursive: true, force: true });
}
if (failure) exitFailing(failure.message);
