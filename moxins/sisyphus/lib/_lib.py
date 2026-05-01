import json
import os
import sys

from atlassian import Jira

# Vendored marklas — see ../_vendor/VENDOR.md for source/version.
sys.path.insert(0, os.path.join(os.path.dirname(os.path.realpath(__file__)), "_vendor"))
from marklas import to_adf as _marklas_to_adf, to_md as _marklas_to_md  # noqa: E402


_REQUIRED_ENV = ["JIRA_URL", "JIRA_USERNAME", "JIRA_API_TOKEN"]


def make_client():
    missing = [v for v in _REQUIRED_ENV if not os.environ.get(v)]
    if missing:
        names = ", ".join(missing)
        emit(f"Jira not configured: missing environment variable(s): {names}")
        sys.exit(0)
    return Jira(
        url=os.environ["JIRA_URL"],
        username=os.environ["JIRA_USERNAME"],
        password=os.environ["JIRA_API_TOKEN"],
        cloud=True,
        # v3 is required for ADF body fields; v2 takes wiki markup which our
        # callers (LLMs) tend to produce as Markdown that v2 then renders
        # literally. See issues #203 and #204.
        api_version="3",
    )


def md_to_adf(markdown: str) -> dict:
    """Convert a Markdown string to an Atlassian Document Format dict.

    Returns an ADF document suitable for the v3 description / comment body
    fields. Empty input yields an empty doc rather than failing the request.
    """
    if not markdown:
        return {"version": 1, "type": "doc", "content": []}
    return _marklas_to_adf(markdown)


def adf_to_md(value):
    """Render an ADF value as Markdown for human/LLM consumption.

    - `dict` with shape `{"type": "doc", ...}` → marklas Markdown.
    - `str` → returned unchanged (legacy v2 wiki markup, or already-rendered).
    - falsy → empty string.

    Anything else (unexpected shape) is JSON-stringified so the caller still
    gets *something* readable rather than a crash.
    """
    if not value:
        return ""
    if isinstance(value, str):
        return value
    if isinstance(value, dict) and value.get("type") == "doc":
        return _marklas_to_md(value)
    return json.dumps(value, indent=2)


def render_issue_body_fields(issue):
    """Return a copy of `issue` with ADF body fields rendered to Markdown.

    Mutates a shallow copy of `issue['fields']`, replacing
    `fields.description` with its Markdown rendering when it is an ADF dict.
    Other fields are untouched. Used on the read side so JSON output stays
    LLM-friendly without forcing every caller to know about ADF.
    """
    if not isinstance(issue, dict):
        return issue
    fields = issue.get("fields")
    if not isinstance(fields, dict):
        return issue
    desc = fields.get("description")
    if isinstance(desc, dict) and desc.get("type") == "doc":
        new_fields = dict(fields)
        new_fields["description"] = adf_to_md(desc)
        new_issue = dict(issue)
        new_issue["fields"] = new_fields
        return new_issue
    return issue


def emit(data, mime="text/plain"):
    if not isinstance(data, str):
        data = json.dumps(data, indent=2)
        mime = "application/json"
    print(json.dumps({"content": [{"type": "text", "text": data, "mimeType": mime}]}))
