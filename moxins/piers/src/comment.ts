import { PiersError, gwsJson, printResult, runTool } from "./gws";

// piers.comment: list, reply to, resolve or reopen a Doc's comments (#331
// folded comments-list, comment-reply and comment-resolve into this).

const [subcommand, fileId, commentId, content, pageSize] =
  process.argv.slice(2);

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

await runTool(async (workDir) => {
  if (subcommand === "list") {
    const params = {
      fileId,
      pageSize: Number(pageSize) || 100,
      fields: commentFields,
    };
    printResult(
      await gwsJson(workDir, [
        "drive",
        "comments",
        "list",
        "--params",
        JSON.stringify(params),
      ]),
    );
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
