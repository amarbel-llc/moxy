import { PiersError, gwsBin, gwsJson, printResult, runTool } from "./gws";

// piers.comment: list, reply to, resolve or reopen a Doc's comments (#331
// folded comments-list, comment-reply and comment-resolve into this).

const [subcommand, fileId, commentId, content, pageSize] =
  process.argv.slice(2);

const authorFields = "author(displayName,emailAddress)";
const replyFields = `id,content,action,${authorFields},createdTime`;

function requireCommentId(): string {
  if (!commentId)
    throw new PiersError(`comment ${subcommand} needs comment_id`);
  return commentId;
}

await runTool(async (workDir) => {
  const gws = (args: string[]) => gwsJson(gwsBin, workDir, args);
  switch (subcommand) {
    case "list":
      printResult(
        await gws([
          "drive",
          "comments",
          "list",
          "--params",
          JSON.stringify({
            fileId,
            pageSize: Number(pageSize) || 100,
            fields: `comments(id,content,${authorFields},quotedFileContent,resolved,createdTime,modifiedTime,replies(id,content,${authorFields},createdTime))`,
          }),
        ]),
      );
      return;
    case "reply":
      if (!content) throw new PiersError("comment reply needs content");
    // falls through: a reply is a reply without an action
    case "resolve":
    case "reopen": {
      const body: Record<string, string> = {};
      if (subcommand !== "reply") body.action = subcommand;
      if (content) body.content = content;
      printResult(
        await gws([
          "drive",
          "replies",
          "create",
          "--params",
          JSON.stringify({
            fileId,
            commentId: requireCommentId(),
            fields: replyFields,
          }),
          "--json",
          JSON.stringify(body),
        ]),
      );
      return;
    }
    default:
      throw new PiersError(
        `unknown comment subcommand ${subcommand}: use list, reply, resolve or reopen`,
      );
  }
});
