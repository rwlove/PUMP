#!/usr/bin/env python3
"""
autofix.py — reads CI failure logs, asks Claude to diagnose and fix them,
then writes the fixed files to disk.

Outputs to GITHUB_OUTPUT:
  fixed=true|false
Writes commit message to /tmp/fix_commit_msg.txt when fixed=true.
"""

import os
import re
import json
import sys

try:
    import anthropic
except ImportError:
    print("anthropic package not installed", file=sys.stderr)
    sys.exit(1)


# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
LOG_FILE      = "/tmp/ci_errors.txt"
COMMIT_FILE   = "/tmp/fix_commit_msg.txt"
MAX_LOG_CHARS = 12_000   # keep prompt manageable
MAX_FILES     = 12       # cap on source files sent to Claude
MAX_FILE_LINES = 400     # lines per file


def gh_output(key: str, value: str) -> None:
    path = os.environ.get("GITHUB_OUTPUT", "")
    if path:
        with open(path, "a") as f:
            f.write(f"{key}={value}\n")
    else:
        print(f"[output] {key}={value}")


# ---------------------------------------------------------------------------
# Log parsing helpers
# ---------------------------------------------------------------------------
def extract_candidate_paths(log: str) -> list[str]:
    """
    Pull relative file paths out of CI error lines.
    Handles Kotlin, Go, YAML and generic patterns.
    """
    repo_root = os.path.abspath(".")
    runner_prefix_re = re.compile(
        r"file:///home/runner/work/[^/]+/[^/]+/(.+?)(?::\d+)+(?:\s|$)"
    )
    generic_path_re = re.compile(
        r"(?:^|\s)((?:\w[\w./\-]*/)[\w.\-]+\.(?:kt|go|java|kts|gradle|yml|yaml|py|toml))(?::\d+)*"
    )
    seen: dict[str, None] = {}

    for pattern in (runner_prefix_re, generic_path_re):
        for m in pattern.finditer(log):
            rel = m.group(1).strip().lstrip("/")
            abs_p = os.path.join(repo_root, rel)
            if os.path.isfile(abs_p) and rel not in seen:
                seen[rel] = None

    # Deduplicate while preserving order
    return list(seen.keys())


def read_file_truncated(rel_path: str) -> str | None:
    abs_p = os.path.abspath(rel_path)
    repo_root = os.path.abspath(".")
    # Security: reject paths outside repo
    if not abs_p.startswith(repo_root + os.sep) and abs_p != repo_root:
        return None
    try:
        with open(abs_p, "r", errors="replace") as f:
            lines = f.readlines()
        if len(lines) > MAX_FILE_LINES:
            lines = lines[:MAX_FILE_LINES]
            lines.append(f"\n... (truncated at {MAX_FILE_LINES} lines)\n")
        return "".join(lines)
    except OSError:
        return None


# ---------------------------------------------------------------------------
# Claude interaction
# ---------------------------------------------------------------------------
SYSTEM_PROMPT = """\
You are an expert software engineer embedded in a CI/CD pipeline.
Your job: read the error logs from a failed GitHub Actions build and
return a JSON object that fixes the problem.

Rules:
- Only fix what is clearly broken. Do not refactor unrelated code.
- Return complete file contents (not diffs).
- If you are not confident, set can_fix=false.
- Your entire response must be a single valid JSON object — no markdown fences,
  no explanation outside the JSON.

JSON schema:
{
  "can_fix": true | false,
  "explanation": "one-sentence diagnosis",
  "commit_message": "fix: concise imperative description (≤72 chars)",
  "files": [
    { "path": "repo-relative/path/to/file.ext", "content": "full file text" }
  ]
}
"""


def build_user_message(log: str, file_map: dict[str, str]) -> str:
    parts = [f"## CI failure log\n\n```\n{log}\n```\n"]

    if file_map:
        parts.append("## Relevant source files\n")
        for path, content in file_map.items():
            ext = path.rsplit(".", 1)[-1] if "." in path else ""
            parts.append(f"### `{path}`\n```{ext}\n{content}\n```\n")
    else:
        parts.append("*(No source files could be automatically identified.)*\n")

    parts.append(
        "Diagnose the failure and return the JSON fix object described in the system prompt."
    )
    return "\n".join(parts)


def call_claude(user_msg: str) -> dict:
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    if not api_key:
        raise RuntimeError("ANTHROPIC_API_KEY not set")

    client = anthropic.Anthropic(api_key=api_key)
    response = client.messages.create(
        model="claude-opus-4-5",
        max_tokens=8192,
        system=SYSTEM_PROMPT,
        messages=[{"role": "user", "content": user_msg}],
    )

    raw = response.content[0].text.strip()

    # Strip accidental markdown fences
    raw = re.sub(r"^```(?:json)?\s*", "", raw)
    raw = re.sub(r"\s*```$", "", raw)

    return json.loads(raw)


# ---------------------------------------------------------------------------
# Apply fixes
# ---------------------------------------------------------------------------
def apply_fixes(files: list[dict]) -> list[str]:
    repo_root = os.path.abspath(".")
    changed: list[str] = []
    for entry in files:
        rel = entry.get("path", "").strip().lstrip("/")
        content = entry.get("content", "")
        if not rel or not content:
            print(f"[skip] empty path or content in fix entry")
            continue

        abs_p = os.path.abspath(os.path.join(repo_root, rel))
        if not abs_p.startswith(repo_root + os.sep):
            print(f"[skip] path escapes repo root: {rel}")
            continue

        os.makedirs(os.path.dirname(abs_p), exist_ok=True)
        with open(abs_p, "w") as f:
            f.write(content)
        print(f"[fixed] {rel}")
        changed.append(rel)
    return changed


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
def main() -> None:
    # 1. Load logs
    if not os.path.exists(LOG_FILE):
        print(f"Log file not found: {LOG_FILE}")
        gh_output("fixed", "false")
        return

    with open(LOG_FILE) as f:
        raw_log = f.read()

    if not raw_log.strip():
        print("Log file is empty — nothing to fix.")
        gh_output("fixed", "false")
        return

    log = raw_log[-MAX_LOG_CHARS:]  # keep the tail (most relevant)

    # 2. Find source files referenced in the logs
    candidates = extract_candidate_paths(raw_log)
    print(f"Candidate files: {candidates}")

    file_map: dict[str, str] = {}
    for rel in candidates[:MAX_FILES]:
        content = read_file_truncated(rel)
        if content is not None:
            file_map[rel] = content

    # 3. Ask Claude
    user_msg = build_user_message(log, file_map)
    print(f"Calling Claude (log {len(log)} chars, {len(file_map)} files)…")

    try:
        result = call_claude(user_msg)
    except json.JSONDecodeError as e:
        print(f"Claude returned invalid JSON: {e}")
        gh_output("fixed", "false")
        return
    except Exception as e:
        print(f"Claude API error: {e}")
        gh_output("fixed", "false")
        return

    print(f"Claude says can_fix={result.get('can_fix')}: {result.get('explanation')}")

    if not result.get("can_fix") or not result.get("files"):
        gh_output("fixed", "false")
        return

    # 4. Apply fixes
    changed = apply_fixes(result["files"])
    if not changed:
        print("No files were actually written.")
        gh_output("fixed", "false")
        return

    # 5. Write commit message to a file (avoids multiline GITHUB_OUTPUT issues)
    commit_msg = result.get("commit_message", "fix: auto-fix CI failure").strip()
    commit_msg += "\n\nCo-Authored-By: Claude Opus 4 <noreply@anthropic.com>"
    with open(COMMIT_FILE, "w") as f:
        f.write(commit_msg)

    gh_output("fixed", "true")
    print(f"Done. {len(changed)} file(s) fixed.")


if __name__ == "__main__":
    main()
