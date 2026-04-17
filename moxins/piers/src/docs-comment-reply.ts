import { $ } from "zx";

$.verbose = false;

const [fileId, commentId, content] = process.argv.slice(2);

const params = JSON.stringify({
  fileId,
  commentId,
  fields: "id,content,author(displayName,emailAddress),createdTime",
});

const json = JSON.stringify({ content });

const result = await $`gws drive replies create --params ${params} --json ${json}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
