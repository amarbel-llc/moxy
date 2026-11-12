import { gwsPassthrough } from "../../../moxin-lib/gws-run";

const [max, query] = process.argv.slice(2);

const params = JSON.stringify({
  userId: "me",
  q: query || "is:unread",
  maxResults: Number(max) || 20,
});

await gwsPassthrough([
  "gmail",
  "users",
  "messages",
  "list",
  "--params",
  params,
]);
