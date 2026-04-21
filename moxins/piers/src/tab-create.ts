import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, title, parentTabId, index] = process.argv.slice(2);

const tabProperties: Record<string, unknown> = {};
if (title) tabProperties.title = title;
if (parentTabId) tabProperties.parentTabId = parentTabId;
if (index) tabProperties.index = Number(index);

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [{ addDocumentTab: { tabProperties } }],
});

const result =
  await $`gws docs documents batchUpdate --params ${params} --json ${json}`;

process.stdout.write(
  JSON.stringify({
    content: [
      { type: "text", text: result.stdout, mimeType: "application/json" },
    ],
  }),
);
