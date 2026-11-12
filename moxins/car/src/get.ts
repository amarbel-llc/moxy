import { gwsPassthrough } from "../../../moxin-lib/gws-run";

const [fileId] = process.argv.slice(2);

const params = JSON.stringify({
  fileId,
  fields:
    "id,name,mimeType,size,modifiedTime,createdTime,webViewLink,owners,permissions,description",
});

await gwsPassthrough(["drive", "files", "get", "--params", params]);
