import { $ } from "zx";

$.verbose = false;

const [
  stateArg,
  labelsJson,
  assignee,
  milestone,
  search,
  limitStr,
  outputFormatArg,
] = process.argv.slice(2);

const state = stateArg || "open";
const outputFormat = outputFormatArg || "json";

const limit = parseInt(limitStr || "") || 30;
const fields =
  "number,title,state,labels,assignees,milestone,createdAt,updatedAt";

const args = [
  "issue",
  "list",
  "--state",
  state,
  "--limit",
  String(limit),
  "--json",
  fields,
];

if (labelsJson && labelsJson !== "null") {
  const labels: string[] = JSON.parse(labelsJson);
  args.push("--label", labels.join(","));
}
if (assignee) args.push("--assignee", assignee);
if (milestone) args.push("--milestone", milestone);
if (search) args.push("--search", search);

const raw: any[] = JSON.parse((await $`gh ${args}`).stdout);

let mime: string, text: string;

if (outputFormat === "text") {
  mime = "text/plain";
  const lines = raw.map(
    (i) =>
      `#${i.number}\t${i.state}\t${i.title}\t${(i.labels || []).map((l: any) => l.name).join(",")}`,
  );
  text = lines.join("\n");
} else {
  mime = "application/json";
  text = JSON.stringify(
    raw.map((i) => ({
      number: i.number,
      title: i.title,
      state: i.state,
      labels: (i.labels || []).map((l: any) => l.name),
      assignees: (i.assignees || []).map((a: any) => a.login),
      milestone: i.milestone?.title ?? null,
      created: i.createdAt,
      updated: i.updatedAt,
    })),
  );
}

const result = { content: [{ type: "text", text, mimeType: mime }] };
process.stdout.write(JSON.stringify(result));
