"""Helpers for enriching Jira issue-type errors (#292).

create-issue passes ``issuetype`` straight through to Jira. When the name is
wrong for the project (the classic case: passing ``Sub-task`` to a project
whose subtask type is named ``Subtask``), Jira returns a generic 400 with no
hint about valid names. These helpers turn that into an actionable error
listing the project's real issue types and suggesting the likely intended
one.

Pure dict/string logic — no third-party imports — so it unit-tests without
the atlassian SDK, mirroring ``_validate.py``.
"""

from __future__ import annotations


def is_issuetype_error_body(body) -> bool:
    """True when a parsed Jira error body blames the ``issuetype`` field."""
    if not isinstance(body, dict):
        return False
    errors = body.get("errors")
    return isinstance(errors, dict) and "issuetype" in errors


def extract_issue_type_names(createmeta) -> list:
    """Pull issue-type names out of a createmeta/issuetypes response.

    Tolerates both the paginated ``values`` bean returned by
    ``/rest/api/3/issue/createmeta/{project}/issuetypes`` and a bare
    ``issueTypes`` list. De-dupes preserving first-seen order.
    """
    if not isinstance(createmeta, dict):
        return []
    items = createmeta.get("values")
    if not isinstance(items, list):
        items = createmeta.get("issueTypes")
    if not isinstance(items, list):
        return []
    out: list = []
    seen: set = set()
    for it in items:
        if isinstance(it, dict):
            name = it.get("name")
            if isinstance(name, str) and name and name not in seen:
                seen.add(name)
                out.append(name)
    return out


def _norm(s: str) -> str:
    """Fold case, hyphens, and spaces so ``Sub-task`` == ``Subtask``."""
    return s.replace("-", "").replace(" ", "").lower()


def suggest_issue_type(requested: str, names) -> str | None:
    """Return a project issue type matching ``requested`` modulo case,
    hyphens, and spaces (so ``Sub-task`` <-> ``Subtask``, and ``subtask`` ->
    ``Subtask``), or ``None``.

    Jira issue-type names are case-sensitive, so a byte-exact match means the
    rejection wasn't a spelling variant and there is nothing to suggest. A
    mere case difference, however, is a real rejection worth correcting.
    """
    if any(n == requested for n in names):
        return None
    req_norm = _norm(requested)
    for n in names:
        if _norm(n) == req_norm:
            return n
    return None


def format_issuetype_error(project: str, requested: str, names) -> str:
    """Build the actionable error message for a rejected issue type."""
    lines = [f"sisyphus: Jira rejected issue type {requested!r} for project {project}."]
    if names:
        lines.append(f"  valid issue types: {', '.join(names)}")
        suggestion = suggest_issue_type(requested, names)
        if suggestion:
            lines.append(
                f"  did you mean {suggestion!r}? (issue type names are per-project)"
            )
    else:
        lines.append(
            "  could not list valid types; issue type names are per-project. "
            f"List them with the api tool: GET /rest/api/3/issue/createmeta/{project}/issuetypes"
        )
    return "\n".join(lines)
