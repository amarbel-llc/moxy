import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, rawIncludeTabsContent] = process.argv.slice(2);

const p: Record<string, unknown> = { documentId };
if (rawIncludeTabsContent === "true") p.includeTabsContent = true;

const params = JSON.stringify(p);
const result = await $`gws docs documents get --params ${params}`;

process.stdout.write(
  JSON.stringify({
    content: [
      { type: "text", text: result.stdout, mimeType: "application/json" },
    ],
  }),
);
