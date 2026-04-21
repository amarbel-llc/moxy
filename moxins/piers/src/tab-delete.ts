import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, tabId] = process.argv.slice(2);

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [{ deleteTab: { tabId } }],
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
