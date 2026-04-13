import { $ } from "zx";

$.verbose = false;

const [repoArg, number, fields, outputFormat] = process.argv.slice(2);

let repo = repoArg;
if (!repo.includes("/")) {
  const owner = JSON.parse((await $`gh repo view --json owner`).stdout).owner
    .login;
  repo = `${owner}/${repo}`;
}

const defaultFields =
  "number,title,state,stateReason,body,labels,assignees,milestone,comments,createdAt,updatedAt,url";
const queryFields = fields || defaultFields;

const raw = JSON.parse(
  (await $`gh issue view ${number} -R ${repo} --json ${queryFields}`).stdout,
);

let mime: string, text: string;

if (outputFormat === "json") {
  mime = "application/json";
  text = JSON.stringify(raw);
} else {
  mime = "text/plain";

  const lines = [`# #${raw.number}: ${raw.title}`];
  lines.push(
    `State: ${raw.state}${raw.stateReason ? ` (${raw.stateReason})` : ""}`,
  );

  if (raw.labels?.length)
    lines.push(`Labels: ${raw.labels.map((l: any) => l.name).join(", ")}`);
  if (raw.assignees?.length)
    lines.push(
      `Assignees: ${raw.assignees.map((a: any) => a.login).join(", ")}`,
    );
  if (raw.milestone) lines.push(`Milestone: ${raw.milestone.title}`);

  lines.push(`Created: ${raw.createdAt}`);
  lines.push(`Updated: ${raw.updatedAt}`);
  if (raw.url) lines.push(`URL: ${raw.url}`);

  lines.push("", raw.body || "");

  if (raw.comments?.length) {
    lines.push("", "---", `## Comments (${raw.comments.length})`);
    for (const c of raw.comments) {
      lines.push(`### @${c.author.login} (${c.createdAt})`, c.body, "");
    }
  }

  text = lines.join("\n");
}

const result = { content: [{ type: "text", text, mimeType: mime }] };
process.stdout.write(JSON.stringify(result));
