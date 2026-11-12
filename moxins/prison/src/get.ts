import { gwsPassthrough } from "../../../moxin-lib/gws-run";

const [spreadsheetId, range] = process.argv.slice(2);

const params = JSON.stringify({
  spreadsheetId,
  range: range || "Sheet1",
});

await gwsPassthrough([
  "sheets",
  "spreadsheets",
  "values",
  "get",
  "--params",
  params,
]);
