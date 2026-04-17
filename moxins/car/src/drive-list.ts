import { $ } from "zx";

$.verbose = false;

const [folderId, pageSize] = process.argv.slice(2);

const params = JSON.stringify({
  q: `'${folderId}' in parents and trashed = false`,
  pageSize: Number(pageSize) || 50,
  fields: "files(id,name,mimeType,modifiedTime,size,webViewLink)",
  orderBy: "name",
});

const result = await $`gws drive files list --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
