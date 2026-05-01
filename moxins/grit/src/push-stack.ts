// Push a stacked branch chain with --force-with-lease.
// Two phases:
//   1. Dry-run pre-flight across every branch (atomic gate). If any
//      dry-run fails, no real pushes happen.
//   2. Real push. Stop on first failure.
// Output is a JSON object the agent can parse to drive recovery.
import { $ } from "zx";
$.verbose = false;

type Status = "ok" | "rejected" | "skipped";
interface Result {
  branch: string;
  status: Status;
  reason?: string;
}

const [branchesJson, remoteArg, repo] = process.argv.slice(2);
const remote = remoteArg && remoteArg.length > 0 ? remoteArg : "origin";

if (!branchesJson) {
  process.stderr.write("ERROR: branches argument is required (JSON array of strings)\n");
  process.exit(2);
}

let branches: string[];
try {
  const parsed = JSON.parse(branchesJson);
  if (!Array.isArray(parsed) || parsed.some((b) => typeof b !== "string")) {
    throw new Error("branches must be an array of strings");
  }
  branches = parsed;
} catch (e) {
  process.stderr.write(`ERROR: invalid branches JSON: ${(e as Error).message}\n`);
  process.exit(2);
}

if (repo) process.chdir(repo);

// Reject any main/master in the chain — same guard as grit.push, applied
// up front so we never start pushing a stack that contains a forbidden ref.
for (const b of branches) {
  if (b === "main" || b === "master") {
    process.stderr.write("ERROR: force push to main/master is blocked for safety\n");
    process.exit(1);
  }
}

function fillSkipped(done: Result[]): Result[] {
  const remaining = branches.slice(done.length).map<Result>((b) => ({
    branch: b,
    status: "skipped",
  }));
  return [...done, ...remaining];
}

const dryRun: Result[] = [];
for (const branch of branches) {
  try {
    await $`git push --dry-run --force-with-lease ${remote} ${branch}`;
    dryRun.push({ branch, status: "ok" });
  } catch (e: any) {
    const reason = ((e?.stderr ?? e?.message ?? "") + "").trim();
    dryRun.push({ branch, status: "rejected", reason });
    process.stdout.write(
      JSON.stringify({ phase: "dry-run", results: fillSkipped(dryRun) }, null, 2) + "\n",
    );
    process.exit(1);
  }
}

const real: Result[] = [];
for (const branch of branches) {
  try {
    await $`git push --force-with-lease ${remote} ${branch}`;
    real.push({ branch, status: "ok" });
  } catch (e: any) {
    const reason = ((e?.stderr ?? e?.message ?? "") + "").trim();
    real.push({ branch, status: "rejected", reason });
    process.stdout.write(
      JSON.stringify({ phase: "push", results: fillSkipped(real) }, null, 2) + "\n",
    );
    process.exit(1);
  }
}

process.stdout.write(JSON.stringify({ phase: "push", results: real }, null, 2) + "\n");
