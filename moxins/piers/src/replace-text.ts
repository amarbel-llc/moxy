import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, text, replaceText, revisionId, matchCase] = process.argv.slice(2);

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [
    {
      replaceAllText: {
        containsText: { text, matchCase: matchCase !== "false" },
        replaceText,
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
