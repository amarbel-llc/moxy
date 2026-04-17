import { $ } from "zx";

$.verbose = false;

const [fileId, commentId, action, content] = process.argv.slice(2);

const params = JSON.stringify({
  fileId,
  commentId,
  fields: "id,content,action,author(displayName,emailAddress),createdTime",
});

const body: Record<string, string> = { action: action || "resolve" };
if (content) {
  body.content = content;
}

const json = JSON.stringify(body);
const result = await $`gws drive replies create --params ${params} --json ${json}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
