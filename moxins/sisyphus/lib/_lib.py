import json
import os
import sys

from atlassian import Jira


def make_client():
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
