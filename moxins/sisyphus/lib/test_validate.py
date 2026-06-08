"""Tests for `_validate.validate`.

Run directly: `python3 moxins/sisyphus/lib/test_validate.py`
or via pytest: `pytest moxins/sisyphus/lib/test_validate.py -v`

The validator is pure-dict-walking — no marklas, mistune, or atlassian-python-api
imports — so the test file has no third-party dependencies and can run from
the system python or the wrapped sisyphus python.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import _validate  # noqa: E402

ADFValidationError = _validate.ADFValidationError


def _doc(*content):
    return {"type": "doc", "version": 1, "content": list(content)}


def _para(*inlines):
    return {"type": "paragraph", "content": list(inlines)}


def _text(text, *mark_types):
    node = {"type": "text", "text": text}
    if mark_types:
        node["marks"] = [{"type": t} for t in mark_types]
    return node


def _expect_violations(adf, *fragments):
    """Assert that `validate(adf)` raises with each fragment in the message."""
    try:
        _validate.validate(adf)
    except ADFValidationError as exc:
        msg = str(exc)
        for fragment in fragments:
            assert fragment in msg, f"missing {fragment!r} in:\n{msg}"
        return
    raise AssertionError(f"expected ADFValidationError; got nothing for {adf!r}")


def _expect_clean(adf):
    _validate.validate(adf)  # should not raise


# ── Rule 1: code mark stacking (#241) ──────────────────────────────────────


def test_code_with_strong_is_rejected():
    adf = _doc(_para(_text("main", "strong", "code")))
    _expect_violations(adf, "Inline code", "strong", "main")


def test_code_with_em_is_rejected():
    adf = _doc(_para(_text("snippet", "em", "code")))
    _expect_violations(adf, "Inline code", "em", "snippet")


def test_code_with_link_is_allowed():
    # Per ADF spec, `code` may co-occur with `link` (and only `link`).
    adf = _doc(_para(_text("link-code", "code", "link")))
    _expect_clean(adf)


def test_plain_code_mark_is_allowed():
    adf = _doc(_para(_text("just code", "code")))
    _expect_clean(adf)


def test_strong_alone_is_allowed():
    adf = _doc(_para(_text("just strong", "strong")))
    _expect_clean(adf)


def test_long_code_text_is_truncated_in_message():
    long_text = "a" * 200
    adf = _doc(_para(_text(long_text, "strong", "code")))
    try:
        _validate.validate(adf)
    except ADFValidationError as exc:
        msg = str(exc)
        assert "…" in msg, f"expected ellipsis truncation in: {msg}"
        assert long_text not in msg, "full long text should not appear"
        return
    raise AssertionError("expected ADFValidationError")


# ── Rule 2: blockquote inside listItem (#205) ──────────────────────────────


def test_blockquote_in_list_item_is_rejected():
    adf = _doc(
        {
            "type": "bulletList",
            "content": [
                {
                    "type": "listItem",
                    "content": [
                        _para(_text("item")),
                        {
                            "type": "blockquote",
                            "content": [_para(_text("nested quote inside list"))],
                        },
                    ],
                },
            ],
        },
    )
    _expect_violations(adf, "List item contains a blockquote", "nested quote")


def test_top_level_blockquote_is_allowed():
    adf = _doc({"type": "blockquote", "content": [_para(_text("a quote"))]})
    _expect_clean(adf)


def test_blockquote_sibling_of_list_is_allowed():
    adf = _doc(
        {
            "type": "bulletList",
            "content": [{"type": "listItem", "content": [_para(_text("item"))]}],
        },
        {"type": "blockquote", "content": [_para(_text("a quote"))]},
    )
    _expect_clean(adf)


# ── Rule 3: malformed table (#239) ─────────────────────────────────────────


def test_table_with_no_rows_is_rejected():
    adf = _doc({"type": "table", "content": []})
    _expect_violations(adf, "table")


def test_table_without_tablerow_children_is_rejected():
    adf = _doc(
        {
            "type": "table",
            "content": [
                # GFM-table-misfire: a tableCell directly under table, no row
                {"type": "tableCell", "content": [_para(_text("bogus"))]},
            ],
        },
    )
    _expect_violations(adf, "table")


def test_well_formed_table_is_allowed():
    adf = _doc(
        {
            "type": "table",
            "content": [
                {
                    "type": "tableRow",
                    "content": [
                        {"type": "tableCell", "content": [_para(_text("cell"))]},
                    ],
                },
            ],
        },
    )
    _expect_clean(adf)


# ── Rule 4: codeBlock with marks (#239) ────────────────────────────────────


def test_codeblock_with_marks_is_rejected():
    adf = _doc(
        {
            "type": "codeBlock",
            "attrs": {"language": "diff"},
            "content": [{"type": "text", "text": "-old\n+new\n"}],
            "marks": [{"type": "strong"}],
        },
    )
    _expect_violations(adf, "code block has inline marks")


def test_codeblock_without_marks_is_allowed():
    adf = _doc(
        {
            "type": "codeBlock",
            "attrs": {"language": "diff"},
            "content": [{"type": "text", "text": "-old\n+new\n"}],
        },
    )
    _expect_clean(adf)


# ── Multi-violation collation ──────────────────────────────────────────────


def test_multiple_violations_are_all_reported():
    adf = _doc(
        _para(_text("bad", "strong", "code")),
        {
            "type": "bulletList",
            "content": [
                {
                    "type": "listItem",
                    "content": [
                        {
                            "type": "blockquote",
                            "content": [_para(_text("quote in list"))],
                        },
                    ],
                },
            ],
        },
    )
    _expect_violations(
        adf,
        "Inline code",  # rule 1
        "List item contains a blockquote",  # rule 2
        "1.",  # numbering present
        "2.",
    )


# ── Clean cases ────────────────────────────────────────────────────────────


def test_empty_doc_is_allowed():
    _expect_clean(_doc())


def test_simple_paragraph_is_allowed():
    _expect_clean(_doc(_para(_text("hello world"))))


def test_full_kitchen_sink_clean_doc_is_allowed():
    # Headings, lists, links, code blocks (no marks), task list, image — none
    # of which trigger any rule.
    adf = _doc(
        {"type": "heading", "attrs": {"level": 2}, "content": [_text("Title")]},
        _para(
            _text("Visit "),
            _text("the docs", "link"),
            _text(" or run "),
            _text("foo", "code"),
            _text("."),
        ),
        {
            "type": "bulletList",
            "content": [
                {"type": "listItem", "content": [_para(_text("a"))]},
                {"type": "listItem", "content": [_para(_text("b"))]},
            ],
        },
        {
            "type": "codeBlock",
            "attrs": {"language": "python"},
            "content": [{"type": "text", "text": "print('ok')\n"}],
        },
    )
    _expect_clean(adf)


# ── Marklas round-trip integration tests ──────────────────────────────────
#
# These tests run real Markdown through `marklas.to_adf` (the same path as
# `_lib.md_to_adf`) and then through the validator, confirming that the
# end-to-end pipeline catches the violations before they reach Jira.
# They require the vendored marklas + mistune to be importable.


def _marklas_to_adf_or_skip(md: str):
    """Convert `md` via marklas; return the ADF dict.

    Returns None when marklas or mistune aren't importable (e.g. running from
    a bare system Python without the deps). Tests that call this skip
    gracefully rather than failing.
    """
    import os as _os

    vendor = _os.path.join(_os.path.dirname(_os.path.realpath(__file__)), "_vendor")
    import sys as _sys

    _sys.path.insert(0, vendor)
    try:
        from marklas import to_adf  # type: ignore

        return to_adf(md)
    except ImportError:
        return None


def test_marklas_pipe_in_inline_code_passes():
    """#239: inline code containing pipes — marklas emits a plain code span, not a table."""
    md = "The matched node is `$parent instanceof Foo|Bar|Baz|Quux`, reachable.\n"
    adf = _marklas_to_adf_or_skip(md)
    if adf is None:
        return
    # Validator must not raise — pipe-bearing inline code is valid ADF.
    _expect_clean(adf)


def test_marklas_diff_codeblock_passes():
    """#239: fenced ```diff codeblock — marklas emits a codeBlock with language=diff, no marks."""
    md = "```diff\n-old_call();\n+new_call();\n```\n"
    adf = _marklas_to_adf_or_skip(md)
    if adf is None:
        return
    # Validator must not raise — diff codeBlock with no marks is valid ADF.
    _expect_clean(adf)


def test_marklas_bold_around_code_is_rejected():
    """Fixture 06a: **bold `inline code`** → marklas emits code+strong → validator rejects."""
    md = "A line with **bold around `inline code` here**.\n"
    adf = _marklas_to_adf_or_skip(md)
    if adf is None:
        return  # marklas not available in this environment; skip
    _expect_violations(adf, "Inline code", "strong")


def test_marklas_clean_markdown_passes():
    """Plain paragraph with no violations round-trips without error."""
    md = "A simple paragraph with `inline code` and **bold** separately.\n"
    adf = _marklas_to_adf_or_skip(md)
    if adf is None:
        return
    _expect_clean(adf)


# ── Test runner ────────────────────────────────────────────────────────────


def _run_all():
    tests = [
        (name, fn)
        for name, fn in globals().items()
        if name.startswith("test_") and callable(fn)
    ]
    failures = []
    for name, fn in tests:
        try:
            fn()
        except AssertionError as exc:
            failures.append((name, str(exc)))
        except Exception as exc:
            failures.append((name, f"{type(exc).__name__}: {exc}"))
    print(f"{len(tests) - len(failures)}/{len(tests)} passed")
    for name, msg in failures:
        print(f"  FAIL {name}: {msg}")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(_run_all())
