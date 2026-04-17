import { $ } from "zx";

$.verbose = false;

const [max, query] = process.argv.slice(2);

const params = JSON.stringify({
  userId: "me",
  q: query || "is:unread",
  maxResults: Number(max) || 20,
});

const result = await $`gws gmail users messages list --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
