import { $ } from "zx";

$.verbose = false;

const [fileId, pageSize] = process.argv.slice(2);

const params = JSON.stringify({
  fileId,
  pageSize: Number(pageSize) || 100,
  fields:
    "comments(id,content,author(displayName,emailAddress),quotedFileContent,resolved,createdTime,modifiedTime,replies(id,content,author(displayName,emailAddress),createdTime))",
});

const result = await $`gws drive comments list --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
