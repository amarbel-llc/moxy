import { $ } from "zx";

$.verbose = false;

// IIFE wrapper needed for local `bun build --compile` which doesn't
// support top-level await. The fork's buildBunBinary handles this
// automatically; remove when nixpkgs bun catches up.
(async () => {
  const [flakeRef = "."] = process.argv.slice(2);

  const output = (
    await $`nix flake show --json ${flakeRef} 2>/dev/null`
  ).stdout.trim();

  const result = {
    content: [{ type: "text", text: output, mimeType: "application/json" }],
  };
  process.stdout.write(JSON.stringify(result));
})();
