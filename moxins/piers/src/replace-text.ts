import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, text, replaceText, revisionId, matchCase, tabIds] =
  process.argv.slice(2);

const req: Record<string, unknown> = {
  containsText: { text, matchCase: matchCase !== "false" },
  replaceText,
};
if (tabIds) req.tabsCriteria = { tabIds: tabIds.split(",").filter(Boolean) };

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [
    {
      replaceAllText: req,
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
