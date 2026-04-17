import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [spreadsheetId, range] = process.argv.slice(2);

const params = JSON.stringify({
  spreadsheetId,
  range: range || "Sheet1",
});

const result = await $`gws sheets spreadsheets values get --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
