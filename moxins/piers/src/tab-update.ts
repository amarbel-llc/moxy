import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, tabProperties, fields] = process.argv.slice(2);

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [
    {
      updateDocumentTabProperties: {
        tabProperties: JSON.parse(tabProperties),
        fields,
      },
    },
  ],
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
