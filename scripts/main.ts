import ghIssueGet from "./tools/gh-issue-get.ts";

const tools: Record<string, (args: string[]) => Promise<void>> = {
  "gh-issue-get": ghIssueGet,
};

(async () => {
  const subcommand = process.argv[2];
  const args = process.argv.slice(3);

  if (!subcommand || subcommand === "--help") {
    console.error(`Usage: moxy-scripts <tool> [args...]`);
    console.error(`\nAvailable tools:`);
    for (const name of Object.keys(tools).sort()) {
      console.error(`  ${name}`);
    }
    process.exit(1);
  }

  const tool = tools[subcommand];
  if (!tool) {
    console.error(`Unknown tool: ${subcommand}`);
    console.error(`Run with --help to see available tools`);
    process.exit(1);
  }

  try {
    await tool(args);
  } catch (err: any) {
    const result = {
      content: [{ type: "text", text: err.message || String(err) }],
      isError: true,
    };
    process.stdout.write(JSON.stringify(result));
    process.exit(1);
  }
})();
