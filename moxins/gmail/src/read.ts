import { gwsPassthrough } from "../../../moxin-lib/gws-run";

const [id] = process.argv.slice(2);

const params = JSON.stringify({
  userId: "me",
  id,
  format: "full",
});

await gwsPassthrough(["gmail", "users", "messages", "get", "--params", params]);
