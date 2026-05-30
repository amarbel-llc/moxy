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

class StackPushFailure extends Error {
  constructor(public payload: object) {
    super("stack push failed");
  }
}

async function main(): Promise<object> {
  const [branchesJson, remoteArg, repo] = process.argv.slice(2);
  const remote = remoteArg && remoteArg.length > 0 ? remoteArg : "origin";

  if (!branchesJson) {
    throw new Error("branches argument is required (JSON array of strings)");
  }

  let branches: string[];
  try {
    const parsed = JSON.parse(branchesJson);
    if (!Array.isArray(parsed) || parsed.some((b) => typeof b !== "string")) {
      throw new Error("branches must be an array of strings");
    }
    branches = parsed;
  } catch (e) {
    throw new Error(`invalid branches JSON: ${(e as Error).message}`);
  }

  if (repo) process.chdir(repo);

  // Reject any main/master in the chain — same guard as grit.push, applied
  // up front so we never start pushing a stack that contains a forbidden ref.
  for (const b of branches) {
    if (b === "main" || b === "master") {
      throw new Error("force push to main/master is blocked for safety");
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
      throw new StackPushFailure({
        phase: "dry-run",
        results: fillSkipped(dryRun),
      });
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
      throw new StackPushFailure({ phase: "push", results: fillSkipped(real) });
    }
  }

  return { phase: "push", results: real };
}

try {
  const result = await main();
  process.stdout.write(JSON.stringify(result, null, 2) + "\n");
} catch (e) {
  if (!(e instanceof StackPushFailure)) throw e;
  process.stdout.write(JSON.stringify(e.payload, null, 2) + "\n");
  process.exitCode = 1;
}
