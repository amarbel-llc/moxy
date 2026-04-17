import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [id] = process.argv.slice(2);

const params = JSON.stringify({
  userId: "me",
  id,
  format: "full",
});

const result = await $`gws gmail users messages get --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
