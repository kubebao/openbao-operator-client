#!/usr/bin/env python3
"""Apply or verify the committed policy for this repository through GitHub CLI."""

import argparse
import json
from pathlib import Path
import subprocess
import sys

REPOSITORY = "kubebao/openbao-operator-client"
ROOT = Path(__file__).resolve().parents[1]
POLICY = ROOT / ".github/repository"


def api(endpoint, method="GET", payload=None):
    command = ["gh", "api", f"repos/{REPOSITORY}/{endpoint}".rstrip("/"), "--method", method]
    if payload is not None:
        command += ["--input", "-"]
    result = subprocess.run(command, input=None if payload is None else json.dumps(payload), text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or "GitHub API request failed")
    return json.loads(result.stdout) if result.stdout.strip() else None


def matches(actual, expected):
    if isinstance(expected, dict):
        return isinstance(actual, dict) and all(key in actual and matches(actual[key], value) for key, value in expected.items())
    if isinstance(expected, list):
        return isinstance(actual, list) and len(actual) == len(expected) and all(
            any(matches(item, wanted) for item in actual) for wanted in expected
        )
    return type(actual) is type(expected) and actual == expected


def configure(apply):
    failures = []
    if apply:
        api("vulnerability-alerts", "PUT")
    for filename, endpoint, method in [
        ("settings.json", "", "PATCH"),
        ("actions.json", "actions/permissions", "PUT"),
        ("selected-actions.json", "actions/permissions/selected-actions", "PUT"),
        ("workflow-permissions.json", "actions/permissions/workflow", "PUT"),
    ]:
        desired = json.loads((POLICY / filename).read_text())
        if apply:
            api(endpoint, method, desired)
        if not matches(api(endpoint), desired):
            failures.append(filename)
    if api("")["visibility"] != "public":
        failures.append("public visibility")
    for endpoint in ["vulnerability-alerts", "private-vulnerability-reporting"]:
        if apply and endpoint == "private-vulnerability-reporting":
            api(endpoint, "PUT")
        actual = api(endpoint)
        if endpoint == "private-vulnerability-reporting" and not actual.get("enabled"):
            failures.append(endpoint)
    existing = api("rulesets?per_page=100")
    for filename in ["default.json", "review-approvals.json", "release-tags.json"]:
        desired = json.loads((POLICY / filename).read_text())
        selected = [item for item in existing if item["name"] == desired["name"] and item["source"] == REPOSITORY]
        if len(selected) > 1:
            raise RuntimeError(f"Multiple rulesets named {desired['name']}")
        endpoint = "rulesets" + (f"/{selected[0]['id']}" if selected else "")
        if apply:
            response = api(endpoint, "PUT" if selected else "POST", desired)
            endpoint = f"rulesets/{response['id']}"
        if not selected and not apply:
            failures.append(filename)
        elif not matches(api(endpoint), desired):
            failures.append(filename)
    if failures:
        raise RuntimeError("Repository policy differs: " + ", ".join(failures))
    print(f"Repository policy verified: https://github.com/{REPOSITORY}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--apply", action="store_true")
    arguments = parser.parse_args()
    configure(arguments.apply)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.CalledProcessError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
