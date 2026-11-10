#!/usr/bin/env python3
"""Require each commit author to sign off their commit under the DCO."""

import subprocess
import sys


def signed_off(author, message):
    trailers = subprocess.run(
        ["git", "interpret-trailers", "--parse"], input=message, text=True, capture_output=True, check=True
    ).stdout
    return any(line.casefold() == f"Signed-off-by: {author}".casefold() for line in trailers.splitlines())


def main():
    base, head = sys.argv[1:]
    revision = head if not base or set(base) == {"0"} else f"{base}..{head}"
    commits = subprocess.run(["git", "rev-list", revision], text=True, capture_output=True, check=True).stdout.splitlines()
    failed = []
    for commit in commits:
        record = subprocess.run(
            ["git", "show", "--no-patch", "--format=%an <%ae>%n%B", commit], text=True, capture_output=True, check=True
        ).stdout
        author, message = record.split("\n", 1)
        if not signed_off(author, message):
            failed.append(commit)
    if failed:
        print("Missing author DCO sign-off: " + ", ".join(failed), file=sys.stderr)
        return 1
    print(f"Author DCO sign-off verified for {len(commits)} commits.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
