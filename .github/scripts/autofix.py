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
LOG_FILE       = os.environ.get("CI_LOG_FILE", "/tmp/ci_logs.txt")
COMMIT_FILE    = "/tmp/fix_commit_msg.txt"
MAX_LOG_CHARS  = 16_000  # full log — Claude needs to see every error/warning
MAX_FILES      = 20      # direct error/warning files + sibling context
MAX_FILE_LINES = 500     # lines per file

# Patterns that indicate an actionable warning in the build log.
# Avoids sending Claude a clean build with nothing to do.
WARNING_PATTERNS = [
    re.compile(r"\bwarning\b", re.IGNORECASE),
    re.compile(r"\bw:\s"),                        # Kotlin/Java warning prefix
    re.compile(r"\bdeprecation\b", re.IGNORECASE),
    re.compile(r"Note:.*uses? (unchecked|unsafe)", re.IGNORECASE),
    re.compile(r"Unable to strip"),               # native lib strip warnings
    re.compile(r"\[WARNING\]"),
]


def has_actionable_warnings(log: str) -> bool:
    """Return True if the log contains at least one warning pattern."""
    return any(p.search(log) for p in WARNING_PATTERNS)


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
    Handles Kotlin (e: file:///…), Go, YAML and generic patterns.
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

    return list(seen.keys())


def failing_workflow_file() -> str | None:
    """
    Return the repo-relative path of the workflow file that failed,
    passed in via the FAILED_WORKFLOW_PATH env var set by the workflow step.
    E.g. ".github/workflows/container-publish.yml"
    """
    path = os.environ.get("FAILED_WORKFLOW_PATH", "").strip()
    if not path:
        return None
    abs_p = os.path.abspath(path)
    repo_root = os.path.abspath(".")
    if os.path.isfile(abs_p) and abs_p.startswith(repo_root):
        return path
    return None


def sibling_files(error_paths: list[str], max_extra: int = 8) -> list[str]:
    """
    For each directory that contains an error file, collect all source
    files in the same directory. This lets Claude spot related issues
    (e.g. other deprecated API usages in the same package) without us
    having to know in advance which files are affected.
    """
    repo_root = os.path.abspath(".")
    source_exts = {".kt", ".go", ".java", ".kts", ".gradle", ".py", ".yml", ".yaml", ".toml"}
    seen_dirs: set[str] = set()
    extras: dict[str, None] = {}

    for rel in error_paths:
        dir_rel = os.path.dirname(rel)
        if dir_rel in seen_dirs:
            continue
        seen_dirs.add(dir_rel)

        abs_dir = os.path.join(repo_root, dir_rel)
        try:
            for fname in sorted(os.listdir(abs_dir)):
                _, ext = os.path.splitext(fname)
                if ext not in source_exts:
                    continue
                candidate = os.path.join(dir_rel, fname) if dir_rel else fname
                if candidate not in extras and candidate not in set(error_paths):
                    extras[candidate] = None
                    if len(extras) >= max_extra:
                        return list(extras.keys())
        except OSError:
            pass

    return list(extras.keys())


def read_file_truncated(rel_path: str) -> str | None:
    abs_p = os.path.abspath(rel_path)
    repo_root = os.path.abspath(".")
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
Your job is to analyse a GitHub Actions build log and fix all problems
— both errors that caused the build to fail AND warnings in builds that
succeeded — completely, in a single pass.

## Strategy

1. **Read every line.** Fix errors that broke the build AND warnings
   that indicate future problems (deprecated APIs, unsafe operations,
   strip failures, unchecked casts, etc.). Do not stop at the first one.
2. **Look for patterns.** If one file uses a deprecated API, scan all
   provided source files for the same pattern and fix every occurrence.
3. **Predict follow-on failures.** After fixing reported problems,
   reason about whether your fixes could expose new compile errors
   (wrong import after an API change, missing override, type mismatch).
   Fix those pre-emptively too.
4. **Stay surgical.** Only change what is broken or provably about to
   break. Do not refactor, reformat, or improve unrelated code.

## Third-party GitHub Actions

When a third-party action (`uses: vendor/action@vN`) is the source of
an error or warning — hardcoded tool paths, missing SDK versions, wrong
defaults — **do not try to work around it with inputs or env vars**.
Replace the action entirely with a direct shell implementation (`run:`)
that accomplishes the same task. Shell steps are explicit,
version-independent, and easier to debug.

## Workflow file

The failing workflow YAML is always provided. Treat it as a first-class
fixable file. Infrastructure problems (missing SDK tools, wrong action
versions, bad env vars, unnecessary steps) are fixed there.

## Output

Return a single valid JSON object — no markdown fences, no prose outside
the JSON.

Schema:
{
  "can_fix": true | false,
  "explanation": "concise summary of all problems found and fixes applied",
  "commit_message": "fix: imperative description covering all changes (≤72 chars)",
  "files": [
    { "path": "repo-relative/path/to/file.ext", "content": "complete file text" }
  ]
}

Set can_fix=false only when you genuinely cannot determine a correct fix
AND there are no warnings worth cleaning up.
"""


def build_user_message(
    log: str,
    conclusion: str,
    workflow_file: str | None,
    workflow_content: str | None,
    file_map: dict[str, str],
    sibling_map: dict[str, str],
) -> str:
    label = "CI build log (build FAILED)" if conclusion == "failure" \
            else "CI build log (build SUCCEEDED — fix warnings)"
    parts = [f"## {label}\n\n```\n{log}\n```\n"]

    # Always show the failing workflow YAML — many errors (missing SDK tools,
    # wrong action versions, bad env vars) require editing the workflow itself.
    if workflow_file and workflow_content:
        parts.append(
            f"## Failing workflow file: `{workflow_file}`\n"
            "*(This is the workflow that produced the errors above. "
            "Edit it if the fix belongs here rather than in source code.)*\n"
        )
        parts.append(f"```yaml\n{workflow_content}\n```\n")

    if file_map:
        parts.append("## Source files directly referenced in errors\n")
        for path, content in file_map.items():
            ext = path.rsplit(".", 1)[-1] if "." in path else ""
            parts.append(f"### `{path}`\n```{ext}\n{content}\n```\n")

    if sibling_map:
        parts.append(
            "## Sibling files in the same directories\n"
            "*(Scan these for the same class of error even if not mentioned in the log.)*\n"
        )
        for path, content in sibling_map.items():
            ext = path.rsplit(".", 1)[-1] if "." in path else ""
            parts.append(f"### `{path}`\n```{ext}\n{content}\n```\n")

    if not workflow_content and not file_map and not sibling_map:
        parts.append("*(No source files could be automatically identified.)*\n")

    parts.append(
        "Fix **all** errors visible in the log and any related issues you can "
        "predict in the provided source files. Return the JSON fix object."
    )
    return "\n".join(parts)


def call_claude(user_msg: str) -> dict:
    oauth_token = os.environ.get("CLAUDE_CODE_OAUTH_TOKEN", "")
    if not oauth_token:
        raise RuntimeError("CLAUDE_CODE_OAUTH_TOKEN not set")

    client = anthropic.Anthropic(auth_token=oauth_token)
    response = client.messages.create(
        model="claude-opus-4-5",
        max_tokens=16384,
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
    conclusion = os.environ.get("BUILD_CONCLUSION", "failure")

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

    # For successful builds, only proceed if there are actionable warnings.
    # Avoids sending Claude a clean build with nothing to do.
    if conclusion == "success" and not has_actionable_warnings(raw_log):
        print("Build succeeded with no actionable warnings — nothing to fix.")
        gh_output("fixed", "false")
        return

    print(f"Build conclusion: {conclusion}. Scanning for errors and warnings.")

    # Prefer the tail for failures (errors appear last); use full log for
    # warning-only successful builds so nothing is missed.
    if conclusion == "failure":
        log = raw_log if len(raw_log) <= MAX_LOG_CHARS else raw_log[-MAX_LOG_CHARS:]
    else:
        log = raw_log[:MAX_LOG_CHARS]  # warnings tend to appear early

    # 2. Always include the failing workflow YAML — errors from CI steps like
    #    missing SDK tools or bad action inputs require editing the workflow,
    #    not source code, and won't reference any source file path in the log.
    wf_rel = failing_workflow_file()
    wf_content = read_file_truncated(wf_rel) if wf_rel else None
    print(f"Failing workflow file: {wf_rel or '(not resolved)'}")

    # 3. Find source files directly mentioned in errors
    error_paths = extract_candidate_paths(raw_log)
    # Exclude the workflow file itself — it's already included above
    error_paths = [p for p in error_paths if p != wf_rel]
    print(f"Error files: {error_paths}")

    file_map: dict[str, str] = {}
    for rel in error_paths[:MAX_FILES]:
        content = read_file_truncated(rel)
        if content is not None:
            file_map[rel] = content

    # 4. Collect sibling files from the same directories for broader context
    remaining_slots = MAX_FILES - len(file_map)
    siblings = sibling_files(error_paths, max_extra=remaining_slots) if remaining_slots > 0 else []
    sibling_map: dict[str, str] = {}
    for rel in siblings:
        content = read_file_truncated(rel)
        if content is not None:
            sibling_map[rel] = content

    print(f"Sibling files for context: {list(sibling_map.keys())}")

    # 5. Ask Claude
    user_msg = build_user_message(log, conclusion, wf_rel, wf_content, file_map, sibling_map)
    print(
        f"Calling Claude (log {len(log)} chars, workflow={'yes' if wf_content else 'no'}, "
        f"{len(file_map)} error files, {len(sibling_map)} sibling files)…"
    )

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

    # 6. Apply fixes
    changed = apply_fixes(result["files"])
    if not changed:
        print("No files were actually written.")
        gh_output("fixed", "false")
        return

    # 7. Write commit message (file avoids multiline GITHUB_OUTPUT issues)
    commit_msg = result.get("commit_message", "fix: auto-fix CI failure").strip()
    commit_msg += "\n\nCo-Authored-By: Claude Opus 4 <noreply@anthropic.com>"
    with open(COMMIT_FILE, "w") as f:
        f.write(commit_msg)

    gh_output("fixed", "true")
    print(f"Done. {len(changed)} file(s) fixed.")


if __name__ == "__main__":
    main()
