import { PiersError, gwsBin, gwsJson, printResult, runTool } from "./gws";

// piers.tab: create, update or delete a Doc's tabs (#331 folded tab-create,
// tab-update and tab-delete into this).

const [subcommand, documentId, tabId, title, parentTabId, index, iconEmoji] =
  process.argv.slice(2);

// The tab properties given, by Docs API name.
function givenProperties(): Record<string, unknown> {
  const properties: Record<string, unknown> = {};
  if (title) properties.title = title;
  if (index) properties.index = Number(index);
  if (iconEmoji) properties.iconEmoji = iconEmoji;
  return properties;
}

function request(): Record<string, unknown> {
  switch (subcommand) {
    case "create": {
      const tabProperties = givenProperties();
      if (parentTabId) tabProperties.parentTabId = parentTabId;
      return { addDocumentTab: { tabProperties } };
    }
    case "update": {
      if (!tabId) throw new PiersError("tab update needs tab_id");
      const properties = givenProperties();
      const fields = Object.keys(properties).join(",");
      if (!fields) {
        throw new PiersError(
          "tab update needs one of title, index or icon_emoji",
        );
      }
      return {
        updateDocumentTabProperties: {
          tabProperties: { tabId, ...properties },
          fields,
        },
      };
    }
    case "delete":
      if (!tabId) throw new PiersError("tab delete needs tab_id");
      return { deleteTab: { tabId } };
    default:
      throw new PiersError(
        `unknown tab subcommand ${subcommand}: use create, update or delete`,
      );
  }
}

await runTool(async (workDir) =>
  printResult(
    await gwsJson(gwsBin, workDir, [
      "docs",
      "documents",
      "batchUpdate",
      "--params",
      JSON.stringify({ documentId }),
      "--json",
      JSON.stringify({ requests: [request()] }),
    ]),
  ),
);
