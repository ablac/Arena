#!/usr/bin/env python3
"""Validate the repository agent contract and PR coordination rules."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CANONICAL_MARKER = "<!-- agent-contract:v1 canonical=AGENTS.md -->"
CLAUDE_POINTER = """# Claude Code entrypoint

Read and follow [`AGENTS.md`](AGENTS.md). It is the canonical instruction file
for this repository. Repository-specific details are linked from there.

Do not duplicate or override the shared rules in this file.
"""
TRACKING_RE = re.compile(
    r"(?im)^\s*Tracking\s*:\s*(#\d+|TASK-\d+|[A-Z][A-Z0-9]+-\d+)\s*$"
)
OVERLAP_APPROVAL_RE = re.compile(
    r"(?im)^\s*Overlap-Approved\s*:\s*#(\d+)\s*$"
)


def fail(message: str, failures: list[str]) -> None:
    failures.append(message)
    print(f"::error::{message}")


def structural_check() -> list[str]:
    failures: list[str] = []
    agents_path = ROOT / "AGENTS.md"
    claude_path = ROOT / "CLAUDE.md"
    if not agents_path.is_file():
        fail("AGENTS.md is missing; it is the canonical agent entrypoint", failures)
    else:
        text = agents_path.read_text(encoding="utf-8")
        if CANONICAL_MARKER not in text:
            fail("AGENTS.md is missing the canonical agent-contract marker", failures)
        for required in (
            "Never push directly",
            "one pull request",
            "Tracking: #123",
            "python .github/agent-contract/check.py",
        ):
            if required not in text:
                fail(f"AGENTS.md is missing required rule text: {required}", failures)
    if not claude_path.is_file():
        fail("CLAUDE.md compatibility pointer is missing", failures)
    else:
        actual = claude_path.read_text(encoding="utf-8").replace("\r\n", "\n")
        if actual.rstrip() != CLAUDE_POINTER.rstrip():
            fail(
                "CLAUDE.md drifted from the canonical compatibility pointer; "
                "put shared rules in AGENTS.md",
                failures,
            )
    return failures


def api_get(url: str, token: str):
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "User-Agent": "ablac-agent-contract",
            "X-GitHub-Api-Version": "2022-11-28",
        },
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def paged(url: str, token: str) -> list[dict]:
    rows: list[dict] = []
    separator = "&" if "?" in url else "?"
    for page in range(1, 11):
        batch = api_get(f"{url}{separator}per_page=100&page={page}", token)
        rows.extend(batch)
        if len(batch) < 100:
            break
    return rows


def tracking_tokens(body: str) -> list[str]:
    return [match.group(1).upper() for match in TRACKING_RE.finditer(body or "")]


def tracking_token(body: str) -> str | None:
    matches = tracking_tokens(body)
    return matches[0] if len(matches) == 1 else None


def pr_check(event_path: Path) -> list[str]:
    failures: list[str] = []
    event = json.loads(event_path.read_text(encoding="utf-8"))
    pull = event.get("pull_request")
    if not pull:
        return failures
    token = os.environ.get("GITHUB_TOKEN", "")
    if not token:
        fail("GITHUB_TOKEN is required for pull-request coordination checks", failures)
        return failures
    api_url = os.environ.get("GITHUB_API_URL", "https://api.github.com")
    repo = event["repository"]["full_name"]
    number = int(pull["number"])
    body = pull.get("body") or ""
    try:
        current_files = {
            row["filename"]
            for row in paged(f"{api_url}/repos/{repo}/pulls/{number}/files", token)
        }
        current_matches = tracking_tokens(body)
        current_tracking = current_matches[0] if len(current_matches) == 1 else None
        if len(current_matches) != 1:
            fail(
                "PR body must contain exactly one `Tracking: #123`, "
                "`Tracking: TASK-123`, or tracker-key line",
                failures,
            )
        approved_overlaps = {
            int(match.group(1)) for match in OVERLAP_APPROVAL_RE.finditer(body)
        }
        open_pulls = paged(f"{api_url}/repos/{repo}/pulls?state=open", token)
        for other in open_pulls:
            other_number = int(other["number"])
            if other_number == number:
                continue
            other_tracking = tracking_token(other.get("body") or "")
            if current_tracking and current_tracking == other_tracking:
                fail(
                    f"PR #{other_number} already tracks {current_tracking}; "
                    "update or supersede that PR instead of creating a competing PR",
                    failures,
                )
            other_files = {
                row["filename"]
                for row in paged(
                    f"{api_url}/repos/{repo}/pulls/{other_number}/files", token
                )
            }
            overlap = sorted(current_files & other_files)
            if not overlap:
                continue
            preview = ", ".join(overlap[:8])
            if other_number not in approved_overlaps:
                fail(
                    f"PR #{other_number} overlaps in {len(overlap)} files ({preview}). "
                    "Consolidate the work or obtain explicit human approval and add "
                    f"`Overlap-Approved: #{other_number}`.",
                    failures,
                )
            else:
                print(
                    f"::warning::PR #{other_number} overlaps in {len(overlap)} file(s): "
                    f"{preview}"
                )
    except (urllib.error.URLError, urllib.error.HTTPError, TimeoutError) as exc:
        fail(f"GitHub coordination check failed closed: {exc}", failures)
    return failures


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--event", type=Path)
    args = parser.parse_args()
    failures = structural_check()
    if args.event:
        failures.extend(pr_check(args.event))
    if failures:
        print(f"agent contract: FAIL ({len(failures)} finding(s))")
        return 1
    print("agent contract: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
