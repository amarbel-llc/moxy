import { $ } from "zx";

$.verbose = false;

const [
  query,
  path,
  extension,
  perPage = "30",
  page = "1",
  repoOwnerName,
] = process.argv.slice(2);

async function resolveRepo(): Promise<string> {
  if (repoOwnerName) {
    if (!repoOwnerName.includes("/")) {
      console.error("ERROR: repo_owner_name must be in OWNER/NAME format");
      process.exit(2);
    }
    return repoOwnerName;
  }
  const user = (await $`gh api /user --jq ${".login"}`).stdout.trim();
  const name = (
    await $`gh repo view --json name --jq ${".name"}`
  ).stdout.trim();
  return `${user}/${name}`;
}

const repo = await resolveRepo();

let q = `${query} repo:${repo}`;
if (path) q += ` path:${path}`;
if (extension) q += ` extension:${extension}`;

const raw = JSON.parse(
  (
    await $`gh api search/code --method GET -H ${"Accept: application/vnd.github.text-match+json"} -f q=${q} -f per_page=${perPage} -f page=${page}`
  ).stdout,
);

const result = {
  total_count: raw.total_count,
  items: (raw.items || []).map((i: any) => ({
    name: i.name,
    path: i.path,
    sha: i.sha.slice(0, 7),
    url: i.html_url,
    score: i.score,
    text_matches: (i.text_matches || []).map((tm: any) => ({
      fragment: tm.fragment,
      matches: (tm.matches || []).map((m: any) => ({
        text: m.text,
        indices: m.indices,
      })),
    })),
  })),
};

process.stdout.write(JSON.stringify(result));
