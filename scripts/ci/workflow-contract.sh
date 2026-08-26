#!/usr/bin/env bash
# 校验 workflow 语法、职责分离和第三方 Action 的完整 SHA pin。
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd -P)"
cd "${REPO_ROOT}"

command -v actionlint >/dev/null 2>&1 || { echo "FAIL: actionlint is required" >&2; exit 1; }
actionlint .github/workflows/*.yml
bash -n scripts/ci/*.sh

python3 <<'PY'
import pathlib
import re

root = pathlib.Path(".github/workflows")
expected = {"pr.yml", "integration.yml", "provider-eval.yml"}
present = {path.name for path in root.glob("*.yml")}
missing = sorted(expected - present)
if missing:
    raise SystemExit(f"missing workflows: {missing}")

uses_pattern = re.compile(r"^\s*-?\s*uses:\s*([^\s#]+)")
sha_pattern = re.compile(r"^[^@]+@[0-9a-f]{40}$")
count = 0
for path in sorted(root.glob("*.yml")):
    for number, line in enumerate(path.read_text().splitlines(), 1):
        match = uses_pattern.match(line)
        if not match:
            continue
        value = match.group(1)
        if value.startswith("./"):
            continue
        count += 1
        if not sha_pattern.fullmatch(value):
            raise SystemExit(f"{path}:{number}: external Action is not pinned to a full 40-character SHA: {value}")
if count == 0:
    raise SystemExit("no external Actions were inspected")

pr = (root / "pr.yml").read_text()
integration = (root / "integration.yml").read_text()
provider = (root / "provider-eval.yml").read_text()
if "pull_request:" not in pr:
    raise SystemExit("pr.yml is not a Pull Request workflow")
if "pull_request:" in provider or "push:" in provider:
    raise SystemExit("provider-eval.yml must be manual/scheduled only")
if "workflow_dispatch:" not in provider or "schedule:" not in provider:
    raise SystemExit("provider-eval.yml must define workflow_dispatch and schedule")
if "sentinelops-e2e" not in integration:
    raise SystemExit("integration.yml does not use the isolated sentinelops-e2e project")
PY

echo "PASS: workflow syntax, separation and immutable Action pins"
