import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const [documentId, rawIncludeLinks] = process.argv.slice(2);
const includeLinks = rawIncludeLinks === "true";

const params = JSON.stringify({ documentId, includeTabsContent: true });
const result = await $`gws docs documents get --params ${params}`;
const doc = JSON.parse(result.stdout);

interface Heading {
  level: number;
  text: string;
  headingId: string;
  startIndex: number;
}

interface Link {
  text: string;
  url: string;
  startIndex: number;
}

interface TabOutline {
  id: string;
  title: string;
  headings: Heading[];
  links?: Link[];
  children?: TabOutline[];
}

const HEADING_LEVELS: Record<string, number> = {
  TITLE: 0,
  SUBTITLE: 0,
  HEADING_1: 1,
  HEADING_2: 2,
  HEADING_3: 3,
  HEADING_4: 4,
  HEADING_5: 5,
  HEADING_6: 6,
};

function extractParagraphText(paragraph: any): string {
  return (paragraph.elements || [])
    .map((el: any) => el.textRun?.content || "")
    .join("")
    .trim();
}

function extractFromBody(content: any[]): { headings: Heading[]; links: Link[] } {
  const headings: Heading[] = [];
  const links: Link[] = [];

  for (const element of content) {
    if (!element.paragraph) continue;
    const para = element.paragraph;
    const styleType = para.paragraphStyle?.namedStyleType;

    if (styleType && styleType in HEADING_LEVELS) {
      headings.push({
        level: HEADING_LEVELS[styleType],
        text: extractParagraphText(para),
        headingId: para.paragraphStyle?.headingId || "",
        startIndex: element.startIndex || 0,
      });
    }

    if (includeLinks) {
      for (const el of para.elements || []) {
        const link = el.textRun?.textStyle?.link;
        if (link?.url) {
          links.push({
            text: (el.textRun.content || "").trim(),
            url: link.url,
            startIndex: el.startIndex || 0,
          });
        }
      }
    }
  }

  return { headings, links };
}

function processTab(tab: any): TabOutline {
  const props = tab.tabProperties || {};
  const body = tab.documentTab?.body?.content || [];
  const { headings, links } = extractFromBody(body);

  const outline: TabOutline = {
    id: props.tabId || "",
    title: props.title || "",
    headings,
  };

  if (includeLinks && links.length > 0) {
    outline.links = links;
  }

  if (tab.childTabs?.length > 0) {
    outline.children = tab.childTabs.map(processTab);
  }

  return outline;
}

let tabs: TabOutline[];

if (doc.tabs?.length > 0) {
  tabs = doc.tabs.map(processTab);
} else {
  const body = doc.body?.content || [];
  const { headings, links } = extractFromBody(body);
  const tab: TabOutline = { id: "", title: "", headings };
  if (includeLinks && links.length > 0) {
    tab.links = links;
  }
  tabs = [tab];
}

const outline = {
  title: doc.title || "",
  tabs,
};

process.stdout.write(
  JSON.stringify({
    content: [
      { type: "text", text: JSON.stringify(outline, null, 2), mimeType: "application/json" },
    ],
  }),
);
