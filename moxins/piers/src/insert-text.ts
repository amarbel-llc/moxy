import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, index, text, revisionId, tabId] = process.argv.slice(2);

const location: Record<string, unknown> = { index: Number(index) };
if (tabId) location.tabId = tabId;

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [
    {
      insertText: {
        location,
        text,
      },
    },
  ],
  writeControl: { requiredRevisionId: revisionId },
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
