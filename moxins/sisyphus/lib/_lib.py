import json
import os
import subprocess
import sys
from contextlib import contextmanager

from atlassian import Jira
from requests.exceptions import HTTPError

# Vendored marklas — see ../_vendor/VENDOR.md for source/version.
sys.path.insert(0, os.path.join(os.path.dirname(os.path.realpath(__file__)), "_vendor"))
from marklas import to_adf as _marklas_to_adf, to_md as _marklas_to_md  # noqa: E402

# Local siblings — `lib/` is on sys.path because each `bin/*` script inserts
# it before `import _lib`.
import _validate  # noqa: E402
import _issuetype  # noqa: E402

ADFValidationError = _validate.ADFValidationError

# Burned in at build time by the sisyphus mkMoxin invocation in flake.nix
# (see `extraSubstitutions = { PANDOC = …; LUA_FILTER = …; }`). At dev /
# brew time the placeholder may remain literal; we fall back to a PATH
# lookup of `pandoc` and a path relative to this file in that case.
_PANDOC_BIN = "@PANDOC@"
_LUA_FILTER = "@LUA_FILTER@"


def _resolve_pandoc():
    if _PANDOC_BIN and not _PANDOC_BIN.startswith("@"):
        return _PANDOC_BIN
    return "pandoc"


def _resolve_lua_filter():
    if _LUA_FILTER and not _LUA_FILTER.startswith("@"):
        return _LUA_FILTER
    return os.path.join(
        os.path.dirname(os.path.realpath(__file__)),
        "strip_adf_wrappers.lua",
    )


def _strip_adf_wrappers(md: str) -> str:
    """Run `md` through pandoc + the strip_adf_wrappers Lua filter.

    Cleans up marklas's ADF-only HTML wrappers (mention/status/inlineCard/
    panel) so the final Markdown is native and idiomatic. On any failure
    (pandoc missing, filter error, etc.) returns the input unchanged and
    logs to stderr so the read tool still produces output.
    """
    try:
        proc = subprocess.run(
            [
                _resolve_pandoc(),
                "--from=gfm+raw_html",
                "--to=gfm",
                f"--lua-filter={_resolve_lua_filter()}",
            ],
            input=md,
            text=True,
            capture_output=True,
            check=True,
        )
        return proc.stdout
    except (FileNotFoundError, subprocess.CalledProcessError) as exc:
        sys.stderr.write(
            f"sisyphus: strip_adf_wrappers post-process failed "
            f"({type(exc).__name__}: {exc}); falling back to raw marklas "
            f"output\n"
        )
        return md


_REQUIRED_ENV = ["JIRA_URL", "JIRA_USERNAME", "JIRA_API_TOKEN"]


def _format_http_error(exc: HTTPError) -> str:
    """Return a multi-line string with Jira's response body appended.

    Includes status code and the raw response body (or JSON-pretty when the
    Content-Type is application/json). The caller is responsible for emitting
    the result and exiting.
    """
    lines = [f"sisyphus: Jira returned HTTP {exc.response.status_code}"]
    try:
        body = exc.response.json()
        body_text = json.dumps(body, indent=2)
    except Exception:
        body_text = exc.response.text or "(empty body)"
    lines.append(f"  body: {body_text}")
    return "\n".join(lines)


@contextmanager
def jira_call():
    """Context manager that catches HTTPError and re-raises with Jira body.

    Usage::

        with jira_call():
            result = jira.create_issue(fields=fields)

    On HTTPError the context manager calls `emit` with the formatted error
    and exits the process non-zero, matching the pattern used by
    `md_to_adf` for validator errors.
    """
    try:
        yield
    except HTTPError as exc:
        emit(_format_http_error(exc))
        sys.exit(1)


def _fetch_issue_type_names(jira, project):
    """Best-effort list of a project's valid issue type names via createmeta.

    Uses the authenticated session directly (mirroring bin/api) rather than a
    named SDK method, so it doesn't depend on the atlassian-python-api version.
    Returns [] on any failure — the caller falls back to a generic hint.
    """
    from urllib.parse import urljoin

    url = urljoin(
        jira.url.rstrip("/") + "/",
        f"rest/api/3/issue/createmeta/{project}/issuetypes",
    )
    resp = jira.session.request("GET", url, timeout=30)
    resp.raise_for_status()
    return _issuetype.extract_issue_type_names(resp.json())


def create_issue_with_issuetype_hint(jira, fields, project, issuetype):
    """create_issue, but turn an issuetype-related 400 into an actionable
    error that lists the project's valid issue types and suggests the likely
    intended one (#292).

    The extra createmeta round-trip happens only on the error path; the happy
    path is a plain create_issue. Non-issuetype HTTPErrors fall through to the
    standard formatted-body emit, matching jira_call().
    """
    try:
        return jira.create_issue(fields=fields)
    except HTTPError as exc:
        try:
            body = exc.response.json()
        except Exception:
            body = None
        if not _issuetype.is_issuetype_error_body(body):
            emit(_format_http_error(exc))
            sys.exit(1)
        try:
            names = _fetch_issue_type_names(jira, project)
        except Exception:
            names = []
        emit(_issuetype.format_issuetype_error(project, issuetype, names))
        sys.exit(1)


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


def browse_url(jira, key):
    """Return the human-facing browse URL ``<base>/browse/<KEY>`` for an issue.

    Built from the configured JIRA_URL site base (the same root the SDK client
    is created with), so callers can hand the user a clickable link instead of
    a bare key (#361). Returns "" when no key or base is available, so callers
    can append it unconditionally.
    """
    if not key:
        return ""
    base = (getattr(jira, "url", "") or os.environ.get("JIRA_URL", "")).rstrip("/")
    if not base:
        return ""
    return f"{base}/browse/{key}"


def parse_argv_list(value):
    """Coerce a CLI-arg `value` to a Python list when it looks list-shaped.

    MCP tool inputs declared as `string` arrive verbatim as `sys.argv[N]`,
    even when the LLM passes a JSON array (the moxy native server
    serialises the array back to its literal JSON-string form: e.g. a
    caller passing `["a","b"]` gets `argv[N] == '["a","b"]'`). The Jira
    SDK accepts comma strings or real Python sequences, but not literal
    JSON-array strings — so without coercion v3 sees something like
    `?fields=%5B%22description%22%5D` and silently degrades.

    This helper:
      - returns `None`/empty unchanged for `None`/empty;
      - parses `value` as JSON when it starts with `[`/`{` and yields a
        list — returns the list;
      - otherwise returns `value` unchanged (the SDK and Jira both accept
        comma-separated strings).
    """
    if not value:
        return value
    if isinstance(value, (list, tuple, set)):
        return list(value)
    if not isinstance(value, str):
        return value
    stripped = value.lstrip()
    if not stripped or stripped[0] not in ("[",):
        return value
    try:
        parsed = json.loads(value)
    except (ValueError, TypeError):
        return value
    if isinstance(parsed, list):
        return parsed
    return value


def resolve_assignee(jira, value: str) -> str:
    """Resolve a user-facing assignee string to a Jira accountId.

    Accepts:
      - "@me" or "me" → resolved via `/rest/api/3/myself`.
      - email-shaped (contains "@") → resolved via `/rest/api/3/user/search`
        with the value as the query string.
      - anything else → returned verbatim (assumed to already be an accountId).

    The `/myself` lookup is cached on the `jira` client to avoid repeated
    round-trips for back-to-back `@me` calls in the same process — short-lived
    moxin processes mean this only matters across multiple resolves in one
    invocation.

    On lookup failure, raises `ValueError` with a sisyphus-friendly message.
    """
    if value in ("@me", "me"):
        cached = getattr(jira, "_sisyphus_myself_cache", None)
        if cached is None:
            cached = jira.myself()
            jira._sisyphus_myself_cache = cached
        return cached["accountId"]
    if "@" in value:
        results = jira.user_find_by_user_string(query=value) or []
        if not results:
            msg = (
                f"sisyphus: assignee {value!r} did not match any Jira user. "
                f"Pass an accountId, an email registered with Jira, or '@me'."
            )
            raise ValueError(msg)
        if len(results) > 1:
            names = ", ".join(r.get("displayName", "?") for r in results[:5])
            msg = (
                f"sisyphus: assignee {value!r} matched {len(results)} Jira "
                f"users ({names}). Pass a more specific email or an accountId."
            )
            raise ValueError(msg)
        return results[0]["accountId"]
    return value


def md_to_adf(markdown: str) -> dict:
    """Convert a Markdown string to an Atlassian Document Format dict.

    Returns an ADF document suitable for the v3 description / comment body
    fields. Empty input yields an empty doc rather than failing the request.

    On a known v3 invariant violation (see `_validate` for the rules), emits
    the guidance message via `emit` and exits the process non-zero. The
    intent is to fail with an actionable message before hitting Jira so the
    calling LLM can rewrite the Markdown and retry without going around the
    wrapper. Tests can call `_validate.validate` directly to exercise the
    rules without the exit.
    """
    if not markdown:
        return {"version": 1, "type": "doc", "content": []}
    adf = _marklas_to_adf(markdown)
    try:
        _validate.validate(adf)
    except ADFValidationError as exc:
        emit(f"sisyphus: {exc}")
        sys.exit(1)
    return adf


def adf_to_md(value):
    """Render an ADF value as Markdown for human/LLM consumption.

    - `dict` with shape `{"type": "doc", ...}` → marklas Markdown, then
      passed through `_strip_adf_wrappers` so ADF-only constructs
      (mention/status/inlineCard/panel) become native Markdown instead of
      marklas's round-trip HTML envelopes.
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
        raw = _marklas_to_md(value)
        # Skip the post-process when the raw output has no ADF-only
        # wrappers — saves a pandoc fork on the common case.
        if 'adf="' not in raw:
            return raw
        return _strip_adf_wrappers(raw)
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
