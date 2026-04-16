import json
import os
import sys

from atlassian import Jira


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
    )


def emit(data, mime="text/plain"):
    if not isinstance(data, str):
        data = json.dumps(data, indent=2)
        mime = "application/json"
    print(json.dumps({"content": [{"type": "text", "text": data, "mimeType": mime}]}))
