"""Tests for `_issuetype` (the #292 create-issue issue-type guard).

Run directly: `python3 moxins/sisyphus/lib/test_issuetype.py`
or via pytest: `pytest moxins/sisyphus/lib/test_issuetype.py -v`

`_issuetype` is pure dict/string logic — no atlassian/marklas/requests
imports — so this test has no third-party dependencies, mirroring
test_validate.py.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import _issuetype  # noqa: E402


# ── is_issuetype_error_body ────────────────────────────────────────────────


def test_detects_issuetype_error_body():
    body = {"errors": {"issuetype": "Specify a valid issue type"}}
    assert _issuetype.is_issuetype_error_body(body) is True


def test_ignores_non_issuetype_error_body():
    body = {"errors": {"summary": "is required"}}
    assert _issuetype.is_issuetype_error_body(body) is False


def test_ignores_non_dict_body():
    assert _issuetype.is_issuetype_error_body(None) is False
    assert _issuetype.is_issuetype_error_body("nope") is False
    assert _issuetype.is_issuetype_error_body({"errors": "oops"}) is False


# ── extract_issue_type_names ───────────────────────────────────────────────


def test_extract_from_values_bean():
    meta = {
        "values": [
            {"id": "1", "name": "Task"},
            {"id": "2", "name": "Bug"},
            {"id": "3", "name": "Subtask"},
        ]
    }
    assert _issuetype.extract_issue_type_names(meta) == ["Task", "Bug", "Subtask"]


def test_extract_from_issuetypes_key():
    meta = {"issueTypes": [{"name": "Story"}, {"name": "Epic"}]}
    assert _issuetype.extract_issue_type_names(meta) == ["Story", "Epic"]


def test_extract_dedupes_preserving_order():
    meta = {"values": [{"name": "Task"}, {"name": "Bug"}, {"name": "Task"}]}
    assert _issuetype.extract_issue_type_names(meta) == ["Task", "Bug"]


def test_extract_tolerates_garbage():
    assert _issuetype.extract_issue_type_names(None) == []
    assert _issuetype.extract_issue_type_names({}) == []
    assert _issuetype.extract_issue_type_names({"values": "nope"}) == []
    assert _issuetype.extract_issue_type_names({"values": [{"id": "1"}, 7, {"name": ""}]}) == []


# ── suggest_issue_type ─────────────────────────────────────────────────────


def test_suggest_hyphen_to_no_hyphen():
    assert _issuetype.suggest_issue_type("Sub-task", ["Task", "Subtask"]) == "Subtask"


def test_suggest_no_hyphen_to_hyphen():
    assert _issuetype.suggest_issue_type("Subtask", ["Task", "Sub-task"]) == "Sub-task"


def test_suggest_is_case_insensitive():
    assert _issuetype.suggest_issue_type("subtask", ["Subtask"]) == "Subtask"


def test_no_suggestion_when_exact_match_exists():
    # An exact (case-insensitive) match means the rejection wasn't a spelling
    # variant; don't suggest the same name back.
    assert _issuetype.suggest_issue_type("Subtask", ["Subtask"]) is None


def test_no_suggestion_when_nothing_close():
    assert _issuetype.suggest_issue_type("Banana", ["Task", "Bug"]) is None


# ── format_issuetype_error ─────────────────────────────────────────────────


def test_format_lists_valid_types_and_suggests():
    msg = _issuetype.format_issuetype_error("XORCH", "Sub-task", ["Task", "Bug", "Subtask"])
    assert "Sub-task" in msg
    assert "XORCH" in msg
    assert "Task, Bug, Subtask" in msg
    assert "did you mean 'Subtask'" in msg


def test_format_without_suggestion_omits_did_you_mean():
    msg = _issuetype.format_issuetype_error("XORCH", "Banana", ["Task", "Bug"])
    assert "valid issue types: Task, Bug" in msg
    assert "did you mean" not in msg


def test_format_falls_back_when_no_names():
    msg = _issuetype.format_issuetype_error("XORCH", "Sub-task", [])
    assert "createmeta/XORCH/issuetypes" in msg
    assert "per-project" in msg


# ── _lib wiring smoke (only when the heavy deps are importable) ─────────────


def test_lib_exposes_issuetype_wrapper():
    """Import `_lib` (atlassian/marklas/requests) and confirm the create-issue
    wrapper exists. Skips on a bare Python without the deps, mirroring
    test_validate.py's marklas-skip. Catches syntax/wiring errors in _lib.py
    that the pure tests above can't, since they never import _lib.
    """
    try:
        import _lib  # noqa: F401
    except ImportError:
        return  # deps not available in this environment; skip
    assert hasattr(_lib, "create_issue_with_issuetype_hint")
    assert hasattr(_lib, "_fetch_issue_type_names")


# ── Test runner ────────────────────────────────────────────────────────────


def _run_all():
    tests = [(name, fn) for name, fn in globals().items() if name.startswith("test_") and callable(fn)]
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
