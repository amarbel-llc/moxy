// POC (moxy#456/#457/#453): fixture and writer invocation shared by the
// offline and live drivers. Run under the zx CLI, which provides `$`.
import { dirname, join } from "path";
import { fileURLToPath } from "url";

export const POC_DIR = dirname(fileURLToPath(import.meta.url));
export const WRITER = join(POC_DIR, "docs-requests.lua");
export const GWS = join(POC_DIR, "result-gws/bin/gws");
export const REMOTE_IMAGE =
  "https://www.google.com/images/branding/googlelogo/1x/googlelogo_color_272x92dp.png";
export const LOCAL_IMAGE = "pixel.png";
export const ONE_PIXEL_PNG_BASE64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==";

export const FIXTURE = `# Heading one

Intro with **bold**, *italic*, \`inline code\`, ~~strike~~ and a [link](https://example.com).

Emoji 🎉 then **after-emoji** bold.

## Code

\`\`\`sh
echo "hello"
  indented line
\twith a tab
\`\`\`

\`\`\`inline span\`\`\` starts this line and is not a fence.

A stray \` backtick then ![img](${LOCAL_IMAGE}) reference.

- item one
- item two
  - nested **bold**
  - nested two

    \`\`\`toml
    key = "value"
    \`\`\`

- [ ] todo item
- [x] done item

1. first
2. second

   \`\`\`
   interior code
   \`\`\`
3. third

| Name | Value |
|:-----|------:|
| alpha | \`1\` |
| **beta** | 2 |

Text with a note.[^1]

Remote ![remote](${REMOTE_IMAGE}) and local ![local](${LOCAL_IMAGE}).

\`\`\`plantuml
Alice -> Bob: hello
\`\`\`

> Quote line
>
> \`\`\`
> quoted fence
> \`\`\`

---

Final paragraph.

[^1]: A footnote with *emph*.
`;

// The writer's output: {requests, footnotes}.
export async function writerOutput(markdown) {
  const out = await $({
    input: markdown,
    verbose: false,
  })`pandoc --preserve-tabs -f gfm -t ${WRITER}`;
  return JSON.parse(out.stdout);
}

export function fail(message) {
  console.log(`FAIL: ${message}`);
  process.exit(1);
}
