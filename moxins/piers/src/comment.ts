import { PiersError, gwsJson, printResult, runTool } from "./gws";

// piers.comment: list, reply to, resolve or reopen a Doc's comments (#331
// folded comments-list, comment-reply and comment-resolve into this).

const [subcommand, fileId, commentId, content, pageSize, pageToken] =
  process.argv.slice(2);

// Drive's comments.list returns at most 100 comments a page.
const drivePageMax = 100;

const authorFields = "author(displayName,emailAddress)";
const commentFields = `comments(id,content,${authorFields},quotedFileContent,resolved,createdTime,modifiedTime,replies(id,content,${authorFields},createdTime))`;
const replyFields = `id,content,action,${authorFields},createdTime`;

// The body of the reply a non-list subcommand posts: a reply carries
// content; resolve and reopen carry the action, and optionally content.
function replyBody(): Record<string, string> {
  if (!["reply", "resolve", "reopen"].includes(subcommand)) {
    throw new PiersError(
      `unknown comment subcommand ${subcommand}: use list, reply, resolve or reopen`,
    );
  }
  if (!commentId) {
    throw new PiersError(`comment ${subcommand} needs comment_id`);
  }
  if (subcommand === "reply" && !content) {
    throw new PiersError("comment reply needs content");
  }
  const body: Record<string, string> = {};
  if (subcommand !== "reply") body.action = subcommand;
  if (content) body.content = content;
  return body;
}

// Up to page_size comments (default 100), following Drive's pages; when more
// exist, nextPageToken is returned, and passing it as page_token resumes
// there (#468).
async function listComments(
  workDir: string,
): Promise<{ comments: unknown[]; nextPageToken?: string }> {
  const limit = Number(pageSize) || 100;
  const comments: unknown[] = [];
  let token: string | undefined = pageToken || undefined;
  do {
    const params: Record<string, unknown> = {
      fileId,
      pageSize: Math.min(drivePageMax, limit - comments.length),
      fields: `nextPageToken,${commentFields}`,
    };
    if (token) params.pageToken = token;
    const page = await gwsJson(workDir, [
      "drive",
      "comments",
      "list",
      "--params",
      JSON.stringify(params),
    ]);
    comments.push(...(page.comments ?? []));
    token = page.nextPageToken;
  } while (token && comments.length < limit);
  return token ? { comments, nextPageToken: token } : { comments };
}

await runTool(async (workDir) => {
  if (subcommand === "list") {
    printResult(await listComments(workDir));
    return;
  }
  const body = replyBody();
  printResult(
    await gwsJson(workDir, [
      "drive",
      "replies",
      "create",
      "--params",
      JSON.stringify({ fileId, commentId, fields: replyFields }),
      "--json",
      JSON.stringify(body),
    ]),
  );
});
