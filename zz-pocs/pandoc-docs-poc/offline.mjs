// POC phase 2: the writer's requests, checked against the text they insert,
// with no Google calls. JS strings index by UTF-16 code unit, as Docs does,
// so slicing the inserted text by a request's range shows what it styles.
// That holds only for the requests before the first bullet request: bullets
// delete leading tabs and structural ops insert content, moving indexes.
import {
  FIXTURE,
  LOCAL_IMAGE,
  REMOTE_IMAGE,
  writerOutput,
  fail,
} from "./common.mjs";

const { requests, footnotes } = await writerOutput(FIXTURE);
const text = requests[0]?.insertText?.text;
if (text === undefined || requests[0].insertText.location.index !== 1) {
  fail("first request is not an insertText at index 1");
}
const kind = (r) => Object.keys(r).find((k) => k !== "pending");
const firstBullet = requests.findIndex((r) => r.createParagraphBullets);
const preBullet = requests.slice(0, firstBullet);
const afterStyling = requests.slice(firstBullet);

// Inserted text plus the body's pre-existing final newline.
const bodyEnd = 1 + text.length + 1;
const covered = (range) =>
  (text + "\n").slice(range.startIndex - 1, range.endIndex - 1);

for (const request of preBullet) {
  const range = request[kind(request)].range;
  if (!range) continue;
  if (!(
    range.startIndex >= 1 &&
    range.startIndex < range.endIndex &&
    range.endIndex <= bodyEnd
  )) {
    fail(`range out of bounds: ${JSON.stringify(request)}`);
  }
}

const textStyled = (predicate) =>
  preBullet
    .filter(
      (r) =>
        r.updateTextStyle &&
        r.updateTextStyle.fields !== "*" &&
        predicate(r.updateTextStyle.textStyle),
    )
    .map((r) => covered(r.updateTextStyle.range));
const paragraphsStyled = (predicate) =>
  preBullet
    .filter(
      (r) =>
        r.updateParagraphStyle &&
        r.updateParagraphStyle.fields !== "*" &&
        predicate(r.updateParagraphStyle.paragraphStyle),
    )
    .map((r) => covered(r.updateParagraphStyle.range));

const expectIncludes = (what, list, value) => {
  if (!list.includes(value))
    fail(
      `${what}: expected ${JSON.stringify(value)} in ${JSON.stringify(list)}`,
    );
};

const bold = textStyled((s) => s.bold);
expectIncludes("bold", bold, "bold");
expectIncludes("bold after a surrogate pair", bold, "after-emoji");

const mono = textStyled((s) => s.weightedFontFamily);
expectIncludes("inline code", mono, "inline code");
expectIncludes(
  "fenced sh block",
  mono,
  'echo "hello"\v  indented line\v\twith a tab',
);
expectIncludes("fence nested 4 deep in a list (#453)", mono, 'key = "value"');
expectIncludes("fence inside a blockquote (#453)", mono, "quoted fence");
expectIncludes(
  "inline triple-backtick span at line start (#453)",
  mono,
  "inline span",
);
expectIncludes("fence inside an ordered list item", mono, "interior code");

const shaded = paragraphsStyled((s) => s.shading);
if (shaded.length !== 4)
  fail(`expected 4 shaded code paragraphs, got ${JSON.stringify(shaded)}`);

expectIncludes(
  "heading 1",
  paragraphsStyled((s) => s.namedStyleType === "HEADING_1"),
  "Heading one",
);
expectIncludes(
  "heading 2",
  paragraphsStyled((s) => s.namedStyleType === "HEADING_2"),
  "Code",
);
expectIncludes(
  "blockquote",
  paragraphsStyled((s) => s.borderLeft),
  "Quote line",
);

if (!text.includes("A stray ` backtick then  reference.")) {
  fail(
    "stray backtick paragraph not rendered as prose with the image taken out",
  );
}
if (!text.includes("starts this line and is not a fence.")) {
  fail("prose after the inline triple-backtick span is missing");
}
if (!text.includes("\tnested ")) fail("nested list item lacks its leading tab");

// Task list: checkbox preset, glyphs gone, done item struck through.
if (text.includes("☐") || text.includes("☒"))
  fail("task checkbox glyph left in the text");
expectIncludes(
  "done task struck through",
  textStyled((s) => s.strikethrough),
  "done item",
);

const bullets = afterStyling
  .filter((r) => r.createParagraphBullets)
  .map((r) => r.createParagraphBullets);
const presets = bullets.map((b) => b.bulletPreset);
for (const preset of [
  "BULLET_DISC_CIRCLE_SQUARE",
  "NUMBERED_DECIMAL_ALPHA_ROMAN",
  "BULLET_CHECKBOX",
]) {
  if (!presets.includes(preset))
    fail(`missing bullet preset ${preset}: ${JSON.stringify(presets)}`);
}
for (let i = 1; i < bullets.length; i++) {
  if (bullets[i].range.startIndex >= bullets[i - 1].range.startIndex) {
    fail("bullet requests are not ordered last-first");
  }
}
// Ordered list: one bullet request across the interior code block, so the
// list keeps one id and its numbering continues.
const ordered = bullets.find(
  (b) => b.bulletPreset === "NUMBERED_DECIMAL_ALPHA_ROMAN",
);
const orderedText = covered(ordered.range);
if (!(
  orderedText.startsWith("first") &&
  orderedText.includes("interior code") &&
  orderedText.endsWith("third")
)) {
  fail(`ordered bullet range covers ${JSON.stringify(orderedText)}`);
}
if (!afterStyling.some((r) => r.deleteParagraphBullets))
  fail("interior code paragraph is not taken off the list");

// Structural ops, each anchored at a position, run last-position first.
const anchors = afterStyling
  .filter((r) => r.insertTable || r.insertInlineImage || r.createFootnote)
  .filter(
    (r) => !r.insertInlineImage || r.insertInlineImage.location.index > 0,
  );
const tableAt = afterStyling.findIndex((r) => r.insertTable);
const topLevel = anchors.map((r) => r[kind(r)].location.index);
for (let i = 1; i < topLevel.length; i++) {
  if (topLevel[i] > topLevel[i - 1])
    fail(`structural ops not in descending order: ${JSON.stringify(topLevel)}`);
}

const table = afterStyling[tableAt].insertTable;
if (table.rows !== 3 || table.columns !== 2)
  fail(`table is ${table.rows}x${table.columns}`);
const cellTexts = afterStyling
  .slice(tableAt + 1)
  .filter((r) => r.insertText)
  .map((r) => r.insertText);
if (
  JSON.stringify(cellTexts.map((c) => c.text)) !==
  JSON.stringify(["2", "beta", "1", "alpha", "Value", "Name"])
) {
  fail(`cell fill order/text: ${JSON.stringify(cellTexts.map((c) => c.text))}`);
}
// Docs layout of an empty 3x2 table inserted at q: the table starts at q+1,
// cell (r, c) content at q+4 + r*(1+2*2) + 2c.
const q = table.location.index;
const expectedIndexes = [
  [2, 1],
  [2, 0],
  [1, 1],
  [1, 0],
  [0, 1],
  [0, 0],
].map(([r, c]) => q + 4 + r * 5 + 2 * c);
if (
  JSON.stringify(cellTexts.map((c) => c.location.index)) !==
  JSON.stringify(expectedIndexes)
) {
  fail(
    `cell indexes ${JSON.stringify(cellTexts.map((c) => c.location.index))}, expected ${JSON.stringify(expectedIndexes)}`,
  );
}
if (!afterStyling.some((r) => r.updateTableCellStyle))
  fail("header row is not shaded");

// The table is anchored at the newline ending "third" (where it stands once
// bullets have removed the nested items' leading tabs), and the old newline,
// now an empty paragraph after the 3x2 table, is deleted.
const thirdNewline = text.indexOf("third") + 1 + "third".length;
const tabsRemovedBefore = (
  text.slice(0, text.indexOf("third")).match(/\n\t/g) ?? []
).length;
if (q !== thirdNewline - tabsRemovedBefore)
  fail(`table anchored at ${q}, expected ${thirdNewline - tabsRemovedBefore}`);
const deleteAfter = afterStyling[tableAt + 1].deleteContentRange?.range;
if (
  !deleteAfter ||
  deleteAfter.startIndex !== q + 1 + 2 + 3 * 5 ||
  deleteAfter.endIndex !== deleteAfter.startIndex + 1
) {
  fail(
    `newline after the table not deleted: ${JSON.stringify(afterStyling[tableAt + 1])}`,
  );
}

const images = afterStyling.filter((r) => r.insertInlineImage);
const pendingKinds = images
  .map((r) => r.pending?.kind ?? r.insertInlineImage.uri)
  .sort();
if (
  JSON.stringify(pendingKinds) !==
  JSON.stringify(["file", "file", REMOTE_IMAGE, "plantuml"].sort())
) {
  fail(`images: ${JSON.stringify(pendingKinds)}`);
}
if (!images.some((r) => r.pending?.path === LOCAL_IMAGE))
  fail("local image path not carried");

if (footnotes.length !== 1 || !afterStyling.some((r) => r.createFootnote))
  fail("expected one footnote");
if (footnotes[0].requests[0].insertText.text !== "A footnote with emph.") {
  fail(
    `footnote text: ${JSON.stringify(footnotes[0].requests[0].insertText.text)}`,
  );
}

console.log(
  `PASS: ${requests.length} requests, ${text.length} UTF-16 units of text, ${footnotes.length} footnote`,
);
