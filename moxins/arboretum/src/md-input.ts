// Shared stdin reader for the md-* tools.
//
// These tools take markdown *content* on stdin (moxy pipes the `markdown` arg
// there). moxy resolves a `madder://blobs/<digest>` arg to the blob's bytes
// before piping, but ANY other string — a file path, a `web-fetch://…`
// resource link, an `http(s)://` URL — is piped verbatim. pandoc then parses
// that single line as text, finds zero headings/anchors, and the tool returns
// silently empty (md-toc) or a misleading "not found" (md-section/md-anchor).
// See moxy#387.
//
// readMarkdownStdin() reads stdin and rejects input that is unmistakably an
// unresolved path or URI rather than markdown, with an actionable message.

const URI_SCHEME_RE = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//;

// A bare filesystem path: starts with /, ./, ../ or ~/, single line, no spaces.
// Conservative so it can't misfire on real markdown (which is multi-line, or
// at least not a lone path token).
const BARE_PATH_RE = /^(?:\/|\.\.?\/|~\/)[^\s]*$/;

export async function readMarkdownStdin(): Promise<string> {
  const markdown = await Bun.stdin.text();
  const trimmed = markdown.trim();

  if (!trimmed) {
    throw new Error("no markdown input on stdin");
  }

  // Single-line input that looks like a path or URI is almost certainly the
  // `markdown` arg passed as a reference moxy didn't resolve (only
  // madder://blobs/<digest> is rewritten to content). Fail loud instead of
  // parsing the reference string as markdown.
  if (!trimmed.includes("\n")) {
    if (URI_SCHEME_RE.test(trimmed)) {
      throw new Error(
        `input looks like a URI, not markdown content: ${trimmed}\n` +
          `Only madder://blobs/<digest> URIs are resolved to content. Pass ` +
          `literal markdown, or read the file/resource yourself and pass its text.`,
      );
    }
    if (BARE_PATH_RE.test(trimmed)) {
      throw new Error(
        `input looks like a file path, not markdown content: ${trimmed}\n` +
          `These tools take markdown text on stdin, not a path. Read the file ` +
          `and pass its contents (or pass a madder://blobs/<digest> URI).`,
      );
    }
  }

  return markdown;
}
