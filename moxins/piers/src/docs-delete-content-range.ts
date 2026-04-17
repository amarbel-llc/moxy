import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, startIndex, endIndex, revisionId] = process.argv.slice(2);

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [
    {
      deleteContentRange: {
        range: { startIndex: Number(startIndex), endIndex: Number(endIndex) },
      },
    },
  ],
  writeControl: { requiredRevisionId: revisionId },
});

const result = await $`gws docs documents batchUpdate --params ${params} --json ${json}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
