import { $ } from "zx";

$.verbose = false;

const [documentId, startIndex, endIndex, paragraphStyle, fields, revisionId] =
  process.argv.slice(2);

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [
    {
      updateParagraphStyle: {
        range: { startIndex: Number(startIndex), endIndex: Number(endIndex) },
        paragraphStyle: JSON.parse(paragraphStyle),
        fields,
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
