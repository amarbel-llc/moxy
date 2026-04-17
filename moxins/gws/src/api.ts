import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [command, params] = process.argv.slice(2);

const args = command.split(/\s+/);
if (params) {
  args.push("--params", params);
}

const result = await $`gws ${args}`;

process.stdout.write(
  JSON.stringify({
    content: [{ type: "text", text: result.stdout, mimeType: "application/json" }],
  }),
);
