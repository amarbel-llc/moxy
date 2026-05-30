import { $ } from "zx";
import { resolveRepo } from "./resolve-repo.ts";

$.verbose = false;

// Surface a clean one-line error (no bun/zx stack trace, see #270) and mark
// the process failed. Returns void so callers can `return err(...)`.
function err(msg: string): void {
  process.stderr.write(msg.endsWith("\n") ? msg : msg + "\n");
  process.exitCode = 1;
}

async function main(): Promise<void> {
  const [numberArg, destinationRepo, repoOwnerName, outputFormat] =
    process.argv.slice(2);

  if (!numberArg) return err("number is required");
  if (!destinationRepo) return err("destination_repo is required");
  if (!destinationRepo.includes("/")) {
    return err("destination_repo must be in OWNER/NAME format");
  }

  const number = parseInt(numberArg, 10);
  if (!Number.isInteger(number)) {
    return err(`number must be an integer, got: ${numberArg}`);
  }

  const src = await resolveRepo(repoOwnerName);
  const [srcOwner, srcName] = src.split("/");
  const [dstOwner, dstName] = destinationRepo.split("/");

  // Step 1: resolve the source issue node ID and the destination repo node ID
  // in a single round-trip. Caller input is bound as GraphQL variables (never
  // interpolated into the query text).
  const idQuery = `
query($srcOwner:String!,$srcName:String!,$num:Int!,$dstOwner:String!,$dstName:String!){
  src: repository(owner:$srcOwner,name:$srcName){ issue(number:$num){ id number url } }
  dst: repository(owner:$dstOwner,name:$dstName){ id nameWithOwner }
}`;

  // .nothrow() so we surface gh/GraphQL stderr ourselves instead of a bun/zx
  // stack trace (see #270).
  const idRes =
    await $`gh api graphql -f query=${idQuery} -f srcOwner=${srcOwner} -f srcName=${srcName} -F num=${number} -f dstOwner=${dstOwner} -f dstName=${dstName}`.nothrow();
  if (idRes.exitCode !== 0) {
    process.stderr.write(idRes.stderr);
    process.exitCode = idRes.exitCode ?? 1;
    return;
  }

  const idData = JSON.parse(idRes.stdout).data;
  const issue = idData?.src?.issue;
  const dst = idData?.dst;
  if (!issue)
    return err(`source issue ${src}#${number} not found (or no access)`);
  if (!dst)
    return err(`destination repo ${destinationRepo} not found (or no access)`);

  // Step 2: transfer. GitHub requires src and dst to share an owner.
  const mutation = `
mutation($issueId:ID!,$repoId:ID!){
  transferIssue(input:{issueId:$issueId,repositoryId:$repoId}){
    issue{ number url repository{ nameWithOwner } }
  }
}`;

  const mutRes =
    await $`gh api graphql -f query=${mutation} -f issueId=${issue.id} -f repoId=${dst.id}`.nothrow();
  if (mutRes.exitCode !== 0) {
    process.stderr.write(mutRes.stderr);
    process.exitCode = mutRes.exitCode ?? 1;
    return;
  }

  const transferred = JSON.parse(mutRes.stdout).data.transferIssue.issue;

  let mime: string, text: string;
  if (outputFormat === "json") {
    mime = "application/json";
    text = JSON.stringify(transferred);
  } else {
    mime = "text/plain";
    text =
      `${src}#${number} → ${transferred.repository.nameWithOwner}#${transferred.number}\n` +
      transferred.url;
  }

  const result = { content: [{ type: "text", text, mimeType: mime }] };
  process.stdout.write(JSON.stringify(result));
}

try {
  await main();
} catch (e) {
  // resolveRepo() and JSON.parse() can throw; surface the message cleanly
  // rather than letting bun print a minified stack trace.
  err(e instanceof Error ? e.message : String(e));
}
