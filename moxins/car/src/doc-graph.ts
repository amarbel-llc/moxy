import { $ } from "zx";
import { mkdtemp, readFile, rm, writeFile } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import {
  extractFileId,
  extractLinksFromHtml,
  classifyUrl,
  shortenExternalUrl,
  classifyExternalUrl,
  mimeColor,
  mimeLabel,
  extractOutline,
  type DocTab,
} from "./gws-links.ts";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

const MAX_NODES = 50;

interface GwsNode {
  id: string;
  label: string;
  mimeType: string;
  webViewLink: string;
  isRoot: boolean;
  inaccessible: boolean;
}

interface ExternalNode {
  url: string;
  label: string;
}

interface StructureNode {
  id: string;
  label: string;
  type: "tab" | "heading";
  level?: number;
  docId: string;
}

interface Edge {
  from: string;
  to: string;
}

const [rawFileId, rawMaxDepth, rawOutput] = process.argv.slice(2);

const rootId = extractFileId(rawFileId);
const maxDepth = Math.min(Math.max(Number(rawMaxDepth) || 1, 1), 5);
const output = rawOutput === "dot" ? "dot" : rawOutput === "svg" ? "svg" : "json";

const gwsNodes = new Map<string, GwsNode>();
const externalNodes = new Map<string, ExternalNode>();
const structureNodes = new Map<string, StructureNode>();
const edges: Edge[] = [];
const edgeSet = new Set<string>();
const visited = new Set<string>();
const queue: { fileId: string; depth: number }[] = [{ fileId: rootId, depth: 0 }];

async function fetchMetadata(fileId: string): Promise<{ name: string; mimeType: string; webViewLink: string }> {
  const params = JSON.stringify({
    fileId,
    fields: "id,name,mimeType,webViewLink",
  });
  const result = await $`gws drive files get --params ${params}`;
  return JSON.parse(result.stdout);
}

async function exportHtml(fileId: string, dir: string): Promise<string> {
  const tmp = join(dir, "export.html");
  const params = JSON.stringify({ fileId, mimeType: "text/html" });
  await $`gws drive files export --params ${params} --output ${tmp}`;
  return readFile(tmp, "utf-8");
}

const DOCS_MIME = "application/vnd.google-apps.document";

async function fetchOutline(fileId: string): Promise<DocTab[]> {
  const params = JSON.stringify({ documentId: fileId, includeTabsContent: true });
  const result = await $`gws docs documents get --params ${params}`;
  return extractOutline(JSON.parse(result.stdout));
}

function addStructureNodes(fileId: string, tabs: DocTab[]) {
  function walkTab(tab: DocTab, parentId: string) {
    const tabId = `${fileId}#tab:${tab.id}`;
    structureNodes.set(tabId, {
      id: tabId,
      label: tab.title || "(untitled tab)",
      type: "tab",
      docId: fileId,
    });
    addEdge(parentId, tabId);

    const stack: { level: number; id: string }[] = [];
    for (const h of tab.headings) {
      const hId = `${fileId}#tab:${tab.id}#h:${h.headingId}`;
      structureNodes.set(hId, {
        id: hId,
        label: h.text,
        type: "heading",
        level: h.level,
        docId: fileId,
      });

      while (stack.length > 0 && stack[stack.length - 1].level >= h.level) {
        stack.pop();
      }
      const parent = stack.length > 0 ? stack[stack.length - 1].id : tabId;
      addEdge(parent, hId);
      stack.push({ level: h.level, id: hId });
    }

    for (const child of tab.children) {
      walkTab(child, tabId);
    }
  }

  for (const tab of tabs) {
    walkTab(tab, fileId);
  }
}

function addEdge(from: string, to: string) {
  if (from === to) return;
  const key = `${from}->${to}`;
  if (edgeSet.has(key)) return;
  edgeSet.add(key);
  edges.push({ from, to });
}

const dir = await mkdtemp(join(tmpdir(), "doc-graph-"));
try {
  while (queue.length > 0 && gwsNodes.size < MAX_NODES) {
    const { fileId, depth } = queue.shift()!;
    if (visited.has(fileId)) continue;
    visited.add(fileId);

    let meta: { name: string; mimeType: string; webViewLink: string };
    try {
      meta = await fetchMetadata(fileId);
    } catch {
      gwsNodes.set(fileId, {
        id: fileId,
        label: `(inaccessible: ${fileId.slice(0, 12)}...)`,
        mimeType: "",
        webViewLink: "",
        isRoot: fileId === rootId,
        inaccessible: true,
      });
      continue;
    }

    gwsNodes.set(fileId, {
      id: fileId,
      label: meta.name,
      mimeType: meta.mimeType,
      webViewLink: meta.webViewLink || "",
      isRoot: fileId === rootId,
      inaccessible: false,
    });

    if (meta.mimeType === DOCS_MIME) {
      try {
        const tabs = await fetchOutline(fileId);
        addStructureNodes(fileId, tabs);
      } catch {}
    }

    if (depth >= maxDepth) continue;

    let html: string;
    try {
      html = await exportHtml(fileId, dir);
    } catch {
      continue;
    }

    const links = extractLinksFromHtml(html);
    for (const link of links) {
      const classified = classifyUrl(link);
      if (classified.type === "gws") {
        addEdge(fileId, classified.fileId);
        if (!visited.has(classified.fileId) && gwsNodes.size < MAX_NODES) {
          queue.push({ fileId: classified.fileId, depth: depth + 1 });
        }
      } else {
        const extKey = classified.url;
        if (!externalNodes.has(extKey)) {
          externalNodes.set(extKey, {
            url: extKey,
            label: shortenExternalUrl(extKey),
          });
        }
        addEdge(fileId, extKey);
      }
    }
  }

  let text: string;
  let mimeType: string;

  if (output === "json") {
    const nodes = [...gwsNodes.values()].map((n) => ({
      id: n.id,
      name: n.label,
      type: n.inaccessible ? "inaccessible" : mimeLabel(n.mimeType),
      mime_type: n.mimeType || undefined,
      url: n.webViewLink || undefined,
      is_root: n.isRoot,
    }));

    const externals = [...externalNodes.values()].map((e) => ({
      url: e.url,
      label: e.label,
      service: classifyExternalUrl(e.url),
    }));

    const structure = [...structureNodes.values()].map((s) => ({
      id: s.id,
      label: s.label,
      type: s.type,
      level: s.level,
      doc_id: s.docId,
    }));

    const typeCounts: Record<string, number> = {};
    for (const n of nodes) {
      typeCounts[n.type] = (typeCounts[n.type] || 0) + 1;
    }
    const serviceCounts: Record<string, number> = {};
    for (const e of externals) {
      serviceCounts[e.service] = (serviceCounts[e.service] || 0) + 1;
    }

    const graph = {
      root: rootId,
      max_depth: maxDepth,
      summary: {
        gws_nodes: gwsNodes.size,
        external_links: externalNodes.size,
        structure_nodes: structureNodes.size,
        edges: edges.length,
        types: typeCounts,
        services: serviceCounts,
      },
      nodes,
      structure_nodes: structure,
      external_links: externals,
      edges: edges.map((e) => ({ from: e.from, to: e.to })),
    };

    text = JSON.stringify(graph, null, 2);
    mimeType = "application/json";
  } else {
    const dotLines: string[] = [];
    dotLines.push("digraph doc_graph {");
    dotLines.push('  rankdir=LR;');
    dotLines.push('  node [shape=box, style=filled, fontname="Helvetica", fontsize=11];');
    dotLines.push('  edge [color="#666666"];');
    dotLines.push("");

    for (const [id, node] of gwsNodes) {
      const escaped = node.label.replace(/"/g, '\\"');
      const color = node.inaccessible ? "#CCCCCC" : mimeColor(node.mimeType);
      const fontcolor = node.inaccessible ? "#666666" : "#FFFFFF";
      const style = node.inaccessible ? "dashed,filled" : "filled";
      const penwidth = node.isRoot ? ', penwidth=2.5' : "";
      dotLines.push(`  "${id}" [label="${escaped}", fillcolor="${color}", fontcolor="${fontcolor}", style="${style}"${penwidth}];`);
    }

    dotLines.push("");
    for (const [url, ext] of externalNodes) {
      const escaped = ext.label.replace(/"/g, '\\"');
      dotLines.push(`  "${url}" [label="${escaped}", fillcolor="#F5F5F5", fontcolor="#999999", style="dashed,filled", fontsize=9];`);
    }

    const docStructure = new Map<string, StructureNode[]>();
    for (const s of structureNodes.values()) {
      const list = docStructure.get(s.docId) || [];
      list.push(s);
      docStructure.set(s.docId, list);
    }

    for (const [docId, nodes] of docStructure) {
      const docLabel = gwsNodes.get(docId)?.label.replace(/"/g, '\\"') || docId.slice(0, 12);
      dotLines.push("");
      dotLines.push(`  subgraph "cluster_${docId}" {`);
      dotLines.push(`    label="${docLabel}";`);
      dotLines.push('    style="dashed"; color="#BBBBBB"; fontsize=9; fontcolor="#999999";');
      for (const s of nodes) {
        const escaped = s.label.replace(/"/g, '\\"');
        if (s.type === "tab") {
          dotLines.push(`    "${s.id}" [label="${escaped}", fillcolor="#E8EAF6", fontcolor="#3949AB", style="filled,rounded", shape=box];`);
        } else {
          dotLines.push(`    "${s.id}" [label="${escaped}", fillcolor="#FAFAFA", fontcolor="#666666", style="filled", fontsize=9];`);
        }
      }
      dotLines.push("  }");
    }

    dotLines.push("");
    for (const edge of edges) {
      dotLines.push(`  "${edge.from}" -> "${edge.to}";`);
    }

    dotLines.push("}");
    const dot = dotLines.join("\n");

    if (output === "svg") {
      const dotFile = join(dir, "graph.dot");
      await writeFile(dotFile, dot);
      const result = await $`dot -Tsvg ${dotFile}`;
      text = result.stdout;
      mimeType = "image/svg+xml";
    } else {
      text = dot;
      mimeType = "text/plain";
    }
  }

  process.stdout.write(
    JSON.stringify({
      content: [{ type: "text", text, mimeType }],
    }),
  );
} finally {
  await rm(dir, { recursive: true, force: true });
}
