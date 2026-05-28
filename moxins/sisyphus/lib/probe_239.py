"""Probe script: inspect what marklas produces for #239 cases.

Run via: just debug-sisyphus-239-probe
"""
import json
import os
import sys

sys.path.insert(0, os.environ["VENDOR"])
from marklas import to_adf  # noqa: E402

print("=== pipe-prose (inline code with pipes) ===")
md1 = "The matched node is `$parent instanceof Foo|Bar|Baz|Quux`, reachable.\n"
adf1 = to_adf(md1)
print(json.dumps(adf1, indent=2))

print()
print("=== diff codeblock ===")
md2 = "```diff\n-old_call();\n+new_call();\n```\n"
adf2 = to_adf(md2)
print(json.dumps(adf2, indent=2))
