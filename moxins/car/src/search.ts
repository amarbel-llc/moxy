import { gwsPassthrough } from "../../../moxin-lib/gws-run";

const [query, pageSize] = process.argv.slice(2);

const params = JSON.stringify({
  q: query,
  pageSize: Number(pageSize) || 20,
  fields: "files(id,name,mimeType,modifiedTime,webViewLink,owners)",
});

await gwsPassthrough(["drive", "files", "list", "--params", params]);
