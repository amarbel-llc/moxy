import { $ } from "zx";

// Shared repo-resolution helper for get-hubbed TypeScript tools.
//
// resolveRepo: return OWNER/NAME derived from the git `origin` remote URL in
// the current working directory. Handles:
//   - SSH:   git@github.com:OWNER/NAME.git  →  OWNER/NAME
//   - HTTPS: https://github.com/OWNER/NAME  →  OWNER/NAME  (with or without .git)
//
// Falls back to `gh api /user` + `gh repo view` when no parseable origin
// remote exists (non-git repos, gist checkouts, etc.).
//
// When repoOwnerName is provided, validates OWNER/NAME format and returns it
// directly — the caller passes their explicit arg here rather than ""/"undefined".

export async function resolveRepo(repoOwnerName?: string): Promise<string> {
  if (repoOwnerName) {
    if (!repoOwnerName.includes("/")) {
      throw new Error("repo_owner_name must be in OWNER/NAME format");
    }
    return repoOwnerName;
  }
  return resolveRepoFromOrigin();
}

async function resolveRepoFromOrigin(): Promise<string> {
  // Try to read the origin remote URL from git.
  let originUrl = "";
  try {
    originUrl = (await $`git remote get-url origin`).stdout.trim();
  } catch {
    // No git repo or no origin remote — fall through to gh fallback.
  }

  if (originUrl) {
    // SSH: git@github.com:OWNER/NAME[.git]
    const sshMatch = originUrl.match(/^git@[^:]+:(.+\/[^/]+?)(?:\.git)?$/);
    if (sshMatch) return sshMatch[1];

    // HTTPS: https://github.com/OWNER/NAME[.git]
    const httpsMatch = originUrl.match(/^https?:\/\/[^/]+\/(.+\/[^/]+?)(?:\.git)?$/);
    if (httpsMatch) return httpsMatch[1];
  }

  // Fallback: no parseable origin — use gh's resolution.
  const user = (await $`gh api /user --jq ${".login"}`).stdout.trim();
  const name = (await $`gh repo view --json name --jq ${".name"}`).stdout.trim();
  return `${user}/${name}`;
}
