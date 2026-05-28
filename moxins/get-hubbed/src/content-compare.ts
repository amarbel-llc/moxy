import { $ } from "zx";
import { resolveRepo } from "./resolve-repo.ts";

$.verbose = false;

const [base, head, perPage = "30", page = "1", repoOwnerName] =
  process.argv.slice(2);

const repo = await resolveRepo(repoOwnerName);

const raw = JSON.parse(
  (
    await $`gh api repos/${repo}/compare/${base}...${head} --method GET -f per_page=${perPage} -f page=${page}`
  ).stdout,
);

const result = {
  status: raw.status,
  ahead_by: raw.ahead_by,
  behind_by: raw.behind_by,
  total_commits: raw.total_commits,
  commits: (raw.commits || []).map((c: any) => ({
    sha: c.sha.slice(0, 7),
    message: c.commit.message.split("\n")[0],
    author: c.commit.author.name,
    date: c.commit.author.date,
  })),
  files: (raw.files || []).map((f: any) => ({
    filename: f.filename,
    status: f.status,
    additions: f.additions,
    deletions: f.deletions,
    changes: f.changes,
  })),
};

process.stdout.write(JSON.stringify(result));
