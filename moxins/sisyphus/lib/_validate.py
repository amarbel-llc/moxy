"""Post-conversion validator for marklas-produced ADF.

`marklas.to_adf` faithfully translates GFM Markdown to ADF without enforcing
Jira v3's content-model and mark-combination invariants. When such a document
is sent to `/rest/api/3/issue/...`, Jira rejects it with a bare `INVALID_INPUT`
and no body detail. See issues #205, #239, #241.

This module walks the produced ADF, collects all known violations, and raises
`ADFValidationError` with a multi-line message describing each one and how to
rewrite the offending Markdown. The intent is that the calling LLM reads the
error and self-corrects on the next call.
"""

from __future__ import annotations


# Marks that may not be combined with `code`. Per Atlassian's spec, `code` may
# only co-occur with `link`. (See https://developer.atlassian.com/cloud/jira/platform/apis/document/marks/code/)
_CODE_INCOMPATIBLE_MARKS = frozenset(
    {"strong", "em", "strike", "underline", "textColor", "backgroundColor", "subsup"},
)


class ADFValidationError(ValueError):
    """Raised when produced ADF violates a known Jira v3 invariant."""


def validate(adf: dict) -> None:
    """Walk `adf` and raise ADFValidationError listing every known violation.

    Returns None on success. The validator is intentionally permissive about
    unknown node types — only the four violations behind issues #205, #239,
    and #241 are checked. New invariants get added here as we discover them.
    """
    violations: list[str] = []
    _walk(adf, violations, in_list_item=False)
    if violations:
        msg = (
            "description rejected — these constructs aren't valid in Jira v3 ADF:\n\n"
            + "\n\n".join(f"  {i}. {v}" for i, v in enumerate(violations, 1))
        )
        raise ADFValidationError(msg)


def _walk(node: dict, violations: list[str], in_list_item: bool) -> None:
    node_type = node.get("type")

    if node_type == "text":
        _check_code_marks(node, violations)
        return

    if node_type == "blockquote" and in_list_item:
        snippet = _first_text_snippet(node) or "(empty)"
        violations.append(
            f'List item contains a blockquote starting "{snippet}". ADF v3 does '
            f"not allow blockquote inside a list item. Hoist the blockquote out "
            f"so it follows the list as a sibling, or rewrite it as a nested "
            f"list / paragraph."
        )

    if node_type == "table":
        _check_table(node, violations)

    if node_type == "codeBlock" and node.get("marks"):
        violations.append(
            "A code block has inline marks attached, which ADF v3 does not "
            "allow on codeBlock nodes. Drop any bold/italic/code formatting "
            "around the fenced code block."
        )

    next_in_list_item = node_type == "listItem"
    for child in node.get("content", []) or []:
        _walk(child, violations, in_list_item=next_in_list_item)


def _check_code_marks(text_node: dict, violations: list[str]) -> None:
    marks = text_node.get("marks") or []
    # Drop marks with no "type" so mark_types is set[str] (not set[str | None]):
    # a None can never be in _CODE_INCOMPATIBLE_MARKS or equal "code", so this
    # is behavior-preserving, and it keeps `sorted(bad)` well-typed.
    mark_types = {
        t for m in marks if isinstance(m, dict) and (t := m.get("type")) is not None
    }
    if "code" not in mark_types:
        return
    bad = mark_types & _CODE_INCOMPATIBLE_MARKS
    if not bad:
        return
    snippet = _short_snippet(text_node.get("text", ""))
    other = ", ".join(sorted(bad))
    violations.append(
        f'Inline code at "{snippet}" also has {other} formatting. ADF v3 '
        f"does not allow combining code with other marks. Rewrite as either "
        f"the code or the surrounding emphasis, not both — for example "
        f"`**bold around `inline`**` becomes `**bold around** `inline`` or "
        f"just `**bold around inline**`."
    )


def _check_table(node: dict, violations: list[str]) -> None:
    rows = node.get("content") or []
    if not rows:
        snippet = _first_text_snippet(node) or "(empty)"
        violations.append(
            f"A table with no rows was produced (likely from a line containing "
            f'`|` characters that GFM parsed as a one-cell table): "{snippet}". '
            f"Rewrite the line so it doesn't look like a GFM table — for "
            f"example wrap pipe-bearing tokens in a sentence form or a code "
            f"block."
        )
        return
    valid_row = any(isinstance(r, dict) and r.get("type") == "tableRow" for r in rows)
    if not valid_row:
        snippet = _first_text_snippet(node) or "(empty)"
        violations.append(
            f'A malformed table was produced (no tableRow children): "{snippet}". '
            f"Rewrite the offending line so it doesn't trigger GFM table parsing."
        )


def _first_text_snippet(node) -> str | None:
    """Return the first text content found by DFS, truncated."""
    if node.get("type") == "text":
        return _short_snippet(node.get("text", ""))
    for child in node.get("content", []) or []:
        snippet = _first_text_snippet(child)
        if snippet:
            return snippet
    return None


def _short_snippet(text: str, limit: int = 60) -> str:
    text = text.strip()
    if len(text) <= limit:
        return text
    return text[: limit - 1].rstrip() + "…"
