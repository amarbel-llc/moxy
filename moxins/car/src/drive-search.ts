import { $ } from "zx";

$.verbose = false;

const [query, pageSize] = process.argv.slice(2);

const params = JSON.stringify({
  q: query,
  pageSize: Number(pageSize) || 20,
  fields: "files(id,name,mimeType,modifiedTime,webViewLink,owners)",
});

const result = await $`gws drive files list --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
