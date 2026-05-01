"""POC: ADF -> MD (via adflux) -> ADF (via marklas), check seam.

This exercises the proposed dual-library split:

    Jira  --[ADF]-->  sisyphus.read   ==(adflux)==>  Markdown shown to LLM
                                                          |
                                                       LLM edits
                                                          v
    Jira  <--[ADF]--  sisyphus.write  ==(marklas)==  edited Markdown

The risk we want to surface: when Markdown produced by adflux contains
ADF-only envelopes (e.g. <!--adf:panel ...-->) for nodes Markdown can't
represent natively (panels, mentions, status), does marklas pass those
through to ADF, drop them, or corrupt them?
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ADF_FIXTURES = HERE / "fixtures-adf"
OUT = HERE / "out" / "cross"


def _adflux_adf_to_md(adf: dict) -> str:
    from adflux import convert

    return convert(json.dumps(adf), src="adf", dst="md")


def _adflux_md_to_adf_dict(md: str) -> dict:
    from adflux import convert

    raw = convert(md, src="md", dst="adf")
    if isinstance(raw, str):
        return json.loads(raw)
    return raw


def _marklas_md_to_adf(md: str) -> dict:
    from marklas import to_adf

    return to_adf(md)


def _marklas_adf_to_md(adf: dict) -> str:
    from marklas import to_md

    return to_md(adf)


def _node_kinds(adf: dict) -> list[str]:
    """Sorted list of node 'type' values found anywhere in the ADF tree."""
    kinds: list[str] = []

    def walk(node):
        if isinstance(node, dict):
            t = node.get("type")
            if t:
                kinds.append(t)
            for v in node.values():
                walk(v)
        elif isinstance(node, list):
            for item in node:
                walk(item)

    walk(adf)
    return sorted(set(kinds))


def run_one(fixture: Path, lib_for_read: str) -> dict:
    """Returns one row of results for the seam table."""
    original_adf = json.loads(fixture.read_text())
    out_dir = OUT / lib_for_read / fixture.stem
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "in.adf.json").write_text(json.dumps(original_adf, indent=2))

    if lib_for_read == "adflux":
        try:
            md = _adflux_adf_to_md(original_adf)
        except Exception as exc:
            (out_dir / "error-read.txt").write_text(f"{type(exc).__name__}: {exc}\n")
            return {"fixture": fixture.stem, "stage": "read", "lib": "adflux", "error": str(exc)}
    elif lib_for_read == "marklas":
        try:
            md = _marklas_adf_to_md(original_adf)
        except Exception as exc:
            (out_dir / "error-read.txt").write_text(f"{type(exc).__name__}: {exc}\n")
            return {"fixture": fixture.stem, "stage": "read", "lib": "marklas", "error": str(exc)}
    else:
        raise ValueError(lib_for_read)

    (out_dir / "intermediate.md").write_text(md)

    # Now write the ADF back via marklas (the proposed write-path lib)
    try:
        round_tripped = _marklas_md_to_adf(md)
    except Exception as exc:
        (out_dir / "error-write.txt").write_text(f"{type(exc).__name__}: {exc}\n")
        return {
            "fixture": fixture.stem,
            "stage": "write",
            "lib": "marklas",
            "error": str(exc),
        }

    (out_dir / "out.adf.json").write_text(json.dumps(round_tripped, indent=2))

    orig_kinds = set(_node_kinds(original_adf))
    out_kinds = set(_node_kinds(round_tripped))
    lost = sorted(orig_kinds - out_kinds)
    gained = sorted(out_kinds - orig_kinds)

    return {
        "fixture": fixture.stem,
        "read_lib": lib_for_read,
        "orig_kinds": sorted(orig_kinds),
        "out_kinds": sorted(out_kinds),
        "lost": lost,
        "gained": gained,
        "lossless_node_set": orig_kinds == out_kinds,
    }


def main() -> int:
    OUT.mkdir(parents=True, exist_ok=True)

    rows = []
    for fixture in sorted(ADF_FIXTURES.glob("*.json")):
        for read_lib in ("adflux", "marklas"):
            row = run_one(fixture, read_lib)
            rows.append(row)

    # Render summary
    summary_path = OUT / "SUMMARY.txt"
    lines = ["fixture\tread_lib\tlost_nodes\tgained_nodes\tnode_set_lossless"]
    for r in rows:
        if "error" in r:
            lines.append(
                f"{r['fixture']}\t{r.get('read_lib','?')}\tERROR ({r['stage']}/{r['lib']}): {r['error']}"
            )
            continue
        lines.append(
            f"{r['fixture']}\t{r['read_lib']}\t{','.join(r['lost']) or '-'}"
            f"\t{','.join(r['gained']) or '-'}\t{r['lossless_node_set']}"
        )
    summary_path.write_text("\n".join(lines) + "\n")
    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main())
