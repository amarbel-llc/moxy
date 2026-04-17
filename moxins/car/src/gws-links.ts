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
