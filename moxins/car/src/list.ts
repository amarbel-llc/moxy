import { gwsPassthrough } from "../../../moxin-lib/gws-run";

const [folderId, pageSize] = process.argv.slice(2);

const params = JSON.stringify({
  q: `'${folderId}' in parents and trashed = false`,
  pageSize: Number(pageSize) || 50,
  fields: "files(id,name,mimeType,modifiedTime,size,webViewLink)",
  orderBy: "name",
});

await gwsPassthrough(["drive", "files", "list", "--params", params]);
