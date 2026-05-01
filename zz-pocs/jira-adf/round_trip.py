"""POC: round-trip every fixture through Markdown -> ADF -> Markdown.

Run via the justfile recipes (which arrange the right ephemeral Python env
through uv). Output goes to zz-pocs/jira-adf/out/<lib>/<fixture>/ with three
artifacts:

    in.md     # the original fixture
    out.adf.json  # the ADF JSON the lib produced from in.md
    out.md    # what the lib produced when handed out.adf.json back

A textual unified diff between in.md and out.md is written to
out/<lib>/<fixture>.diff and a one-line summary is appended to
out/<lib>/SUMMARY.txt.
"""

import argparse
import difflib
import json
import sys
from pathlib import Path


HERE = Path(__file__).resolve().parent
FIXTURES = HERE / "fixtures"
OUT = HERE / "out"


def convert_adflux(md: str) -> tuple[dict, str]:
    """Return (adf, round_tripped_md)."""
    from adflux import convert

    adf = convert(md, src="md", dst="adf")
    md2 = convert(adf, src="adf", dst="md")
    return adf, md2


def convert_marklas(md: str) -> tuple[dict, str]:
    from marklas import to_adf, to_md

    adf = to_adf(md)
    md2 = to_md(adf)
    return adf, md2


CONVERTERS = {
    "adflux": convert_adflux,
    "marklas": convert_marklas,
}


def diff_lines(a: str, b: str, name_a: str, name_b: str) -> str:
    return "".join(
        difflib.unified_diff(
            a.splitlines(keepends=True),
            b.splitlines(keepends=True),
            fromfile=name_a,
            tofile=name_b,
        )
    )


def run_one(lib: str, fixture: Path) -> tuple[bool, str]:
    """Returns (lossless, summary_line)."""
    convert = CONVERTERS[lib]
    md_in = fixture.read_text()

    out_dir = OUT / lib / fixture.stem
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "in.md").write_text(md_in)

    try:
        adf, md_out = convert(md_in)
    except Exception as exc:
        (out_dir / "error.txt").write_text(f"{type(exc).__name__}: {exc}\n")
        return False, f"{fixture.stem}\tERROR\t{type(exc).__name__}: {exc}"

    (out_dir / "out.adf.json").write_text(json.dumps(adf, indent=2, ensure_ascii=False))
    (out_dir / "out.md").write_text(md_out)

    diff = diff_lines(md_in, md_out, "in.md", "out.md")
    (OUT / lib / f"{fixture.stem}.diff").write_text(diff)

    if not diff:
        return True, f"{fixture.stem}\tOK\tlossless"
    in_lines = md_in.count("\n")
    diff_lines_count = sum(1 for ln in diff.splitlines() if ln.startswith(("+ ", "- ")))
    return False, (
        f"{fixture.stem}\tDIFF\t{diff_lines_count} changed lines "
        f"({in_lines} input lines)"
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("lib", choices=list(CONVERTERS))
    parser.add_argument("--fixture", help="single fixture stem (e.g. 01-basic)")
    args = parser.parse_args()

    if args.fixture:
        fixtures = [FIXTURES / f"{args.fixture}.md"]
    else:
        fixtures = sorted(FIXTURES.glob("*.md"))

    summary_dir = OUT / args.lib
    summary_dir.mkdir(parents=True, exist_ok=True)
    summary_path = summary_dir / "SUMMARY.txt"

    rows = []
    all_lossless = True
    for fixture in fixtures:
        lossless, line = run_one(args.lib, fixture)
        rows.append(line)
        if not lossless:
            all_lossless = False

    header = f"# {args.lib} round-trip ({len(rows)} fixtures)\n"
    body = "\n".join(rows)
    summary_path.write_text(header + body + "\n")
    print(header + body)
    return 0 if all_lossless else 1


if __name__ == "__main__":
    sys.exit(main())
