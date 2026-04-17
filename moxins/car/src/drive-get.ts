import { $ } from "zx";

$.verbose = false;

const [fileId] = process.argv.slice(2);

const params = JSON.stringify({
  fileId,
  fields: "id,name,mimeType,size,modifiedTime,createdTime,webViewLink,owners,permissions,description",
});

const result = await $`gws drive files get --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
