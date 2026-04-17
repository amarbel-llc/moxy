const GWS_URL_PATTERNS = [
  /docs\.google\.com\/document\/d\/([a-zA-Z0-9_-]+)/,
  /docs\.google\.com\/spreadsheets\/d\/([a-zA-Z0-9_-]+)/,
  /docs\.google\.com\/presentation\/d\/([a-zA-Z0-9_-]+)/,
  /drive\.google\.com\/file\/d\/([a-zA-Z0-9_-]+)/,
  /drive\.google\.com\/open\?id=([a-zA-Z0-9_-]+)/,
];

export function extractFileId(urlOrId: string): string {
  for (const pattern of GWS_URL_PATTERNS) {
    const match = urlOrId.match(pattern);
    if (match) return match[1];
  }
  return urlOrId;
}

function unwrapGoogleRedirect(url: string): string {
  try {
    const parsed = new URL(url);
    if (parsed.hostname.endsWith("google.com") && parsed.pathname === "/url") {
      const q = parsed.searchParams.get("q");
      if (q) return q;
    }
  } catch {}
  return url;
}

export function extractLinksFromHtml(html: string): string[] {
  const hrefs: string[] = [];
  new HTMLRewriter().on("a[href]", {
    element(el) {
      const href = el.getAttribute("href");
      if (href) hrefs.push(href);
    },
  }).transform(html);
  const seen = new Set<string>();
  const result: string[] = [];
  for (const raw of hrefs) {
    const url = unwrapGoogleRedirect(raw);
    if (url.startsWith("#")) continue;
    if (!seen.has(url)) {
      seen.add(url);
      result.push(url);
    }
  }
  return result;
}

type ClassifiedUrl =
  | { type: "gws"; fileId: string }
  | { type: "external"; url: string };

export function classifyUrl(url: string): ClassifiedUrl {
  for (const pattern of GWS_URL_PATTERNS) {
    const match = url.match(pattern);
    if (match) return { type: "gws", fileId: match[1] };
  }
  return { type: "external", url };
}

const EXTERNAL_SHORTENERS: [RegExp, (m: RegExpMatchArray) => string][] = [
  [/atlassian\.net\/browse\/([A-Z]+-\d+)/, (m) => `Jira: ${m[1]}`],
  [/atlassian\.net\/jira\/.*?selectedIssue=([A-Z]+-\d+)/, (m) => `Jira: ${m[1]}`],
  [/pagerduty\.com\/schedules[/#]([A-Z0-9]+)/, (m) => `PagerDuty: ${m[1]}`],
  [/pagerduty\.com\/service-directory\/(P[A-Z0-9]+)/, (m) => `PagerDuty: ${m[1]}`],
  [/^mailto:(.+)/, (m) => m[1]],
  [/github\.com\/([^/]+\/[^/]+?)(?:\/|$)/, (m) => `GitHub: ${m[1]}`],
  [/miro\.com\/app\/board\/([^/?]+)/, (m) => `Miro: ${m[1]}`],
];

export function shortenExternalUrl(url: string): string {
  for (const [pattern, formatter] of EXTERNAL_SHORTENERS) {
    const match = url.match(pattern);
    if (match) return formatter(match);
  }
  try {
    const parsed = new URL(url);
    const path = parsed.pathname.length > 30
      ? parsed.pathname.slice(0, 27) + "..."
      : parsed.pathname;
    return parsed.hostname + path;
  } catch {
    return url.length > 50 ? url.slice(0, 47) + "..." : url;
  }
}

const MIME_COLORS: Record<string, string> = {
  "application/vnd.google-apps.document": "#4285F4",
  "application/vnd.google-apps.spreadsheet": "#34A853",
  "application/vnd.google-apps.presentation": "#FBBC05",
};

export function mimeColor(mimeType: string): string {
  return MIME_COLORS[mimeType] || "#AAAAAA";
}

const MIME_LABELS: Record<string, string> = {
  "application/vnd.google-apps.document": "document",
  "application/vnd.google-apps.spreadsheet": "spreadsheet",
  "application/vnd.google-apps.presentation": "presentation",
  "application/vnd.google-apps.folder": "folder",
  "application/vnd.google-apps.form": "form",
};

export function mimeLabel(mimeType: string): string {
  return MIME_LABELS[mimeType] || mimeType;
}

export interface DocHeading {
  level: number;
  text: string;
  headingId: string;
}

export interface DocTab {
  id: string;
  title: string;
  headings: DocHeading[];
  children: DocTab[];
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

function extractHeadingsFromBody(content: any[]): DocHeading[] {
  const headings: DocHeading[] = [];
  for (const element of content) {
    if (!element.paragraph) continue;
    const para = element.paragraph;
    const styleType = para.paragraphStyle?.namedStyleType;
    if (styleType && styleType in HEADING_LEVELS) {
      headings.push({
        level: HEADING_LEVELS[styleType],
        text: extractParagraphText(para),
        headingId: para.paragraphStyle?.headingId || "",
      });
    }
  }
  return headings;
}

function processTab(tab: any): DocTab {
  const props = tab.tabProperties || {};
  const body = tab.documentTab?.body?.content || [];
  return {
    id: props.tabId || "t.0",
    title: props.title || "",
    headings: extractHeadingsFromBody(body),
    children: (tab.childTabs || []).map(processTab),
  };
}

export function extractOutline(doc: any): DocTab[] {
  if (doc.tabs?.length > 0) {
    return doc.tabs.map(processTab);
  }
  const body = doc.body?.content || [];
  return [{ id: "t.0", title: "", headings: extractHeadingsFromBody(body), children: [] }];
}

export function classifyExternalUrl(url: string): string {
  if (url.startsWith("mailto:")) return "email";
  for (const [pattern] of EXTERNAL_SHORTENERS) {
    if (pattern.test(url)) {
      if (pattern.source.includes("atlassian")) return "jira";
      if (pattern.source.includes("pagerduty")) return "pagerduty";
      if (pattern.source.includes("github")) return "github";
      if (pattern.source.includes("miro")) return "miro";
    }
  }
  try {
    return new URL(url).hostname;
  } catch {
    return "unknown";
  }
}
