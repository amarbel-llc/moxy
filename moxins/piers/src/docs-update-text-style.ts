import { $ } from "zx";

$.verbose = false;

const [documentId, startIndex, endIndex, textStyle, fields, revisionId] =
  process.argv.slice(2);

const params = JSON.stringify({ documentId });
const json = JSON.stringify({
  requests: [
    {
      updateTextStyle: {
        range: { startIndex: Number(startIndex), endIndex: Number(endIndex) },
        textStyle: JSON.parse(textStyle),
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
