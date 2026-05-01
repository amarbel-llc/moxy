"""POC: render ADF fixtures through marklas, then post-process with pandoc
and our Lua filter, and print before/after diffs.

This is the dev-loop driver for `moxins/sisyphus/lib/strip_adf_wrappers.lua`.
Iterate on the .lua file → `just filter-probe[-one]` to re-render.
"""

import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ADF_FIXTURES = HERE / "fixtures-adf"
LUA_FILTER = (
    HERE.parent.parent
    / "moxins"
    / "sisyphus"
    / "lib"
    / "strip_adf_wrappers.lua"
)


def render(adf_path):
    from marklas import to_md

    adf = json.loads(adf_path.read_text())
    raw_md = to_md(adf)

    proc = subprocess.run(
        [
            "pandoc",
            "--from=gfm+raw_html",
            "--to=gfm",
            f"--lua-filter={LUA_FILTER}",
        ],
        input=raw_md,
        text=True,
        capture_output=True,
    )
    if proc.returncode != 0:
        return raw_md, None, proc.stderr
    return raw_md, proc.stdout, None


def main():
    if not LUA_FILTER.exists():
        print(f"missing lua filter: {LUA_FILTER}", file=sys.stderr)
        return 2

    selected = sys.argv[1] if len(sys.argv) > 1 else None
    fixtures = sorted(ADF_FIXTURES.glob("*.json"))
    if selected:
        fixtures = [f for f in fixtures if f.stem == selected]
        if not fixtures:
            print(f"no fixture named {selected}", file=sys.stderr)
            return 2

    for f in fixtures:
        print("=" * 72)
        print(f"FIXTURE: {f.stem}")
        print("=" * 72)
        raw, filtered, err = render(f)
        print("--- marklas raw ---")
        print(raw, end="")
        if not raw.endswith("\n"):
            print()
        print("--- pandoc + lua filter ---")
        if err:
            print(f"PANDOC FAILED:\n{err}", end="")
        else:
            print(filtered, end="")
            if not filtered.endswith("\n"):
                print()
        print()
    return 0


if __name__ == "__main__":
    sys.exit(main())
