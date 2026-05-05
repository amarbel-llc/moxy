import { $ } from "zx";

$.verbose = false;

const [number, fields, outputFormat, repoOwnerName] = process.argv.slice(2);

async function resolveRepo(): Promise<string> {
  if (repoOwnerName) {
    if (!repoOwnerName.includes("/")) {
      console.error("ERROR: repo_owner_name must be in OWNER/NAME format");
      process.exit(2);
    }
    return repoOwnerName;
  }
  const user = (await $`gh api /user --jq ${".login"}`).stdout.trim();
  const name = (
    await $`gh repo view --json name --jq ${".name"}`
  ).stdout.trim();
  return `${user}/${name}`;
}

const repo = await resolveRepo();

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
