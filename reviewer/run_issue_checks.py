#!/usr/bin/env python3

import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

if __package__:
    from .issue_agent import validate_changes
else:
    from issue_agent import validate_changes

LOG_PATH = Path(os.environ.get("RUNNER_TEMP", "/tmp")) / "agent-validation.log"
SAFE_ENVIRONMENT_KEYS = {
    "CI",
    "GITHUB_ACTIONS",
    "GITHUB_WORKSPACE",
    "HOME",
    "LANG",
    "LC_ALL",
    "NIX_CONF_DIR",
    "NIX_PATH",
    "NIX_SSL_CERT_FILE",
    "PATH",
    "RUNNER_TEMP",
    "SSL_CERT_FILE",
    "TMPDIR",
    "TMP",
    "TEMP",
}


def test_environment() -> dict[str, str]:
    return {
        key: value
        for key, value in os.environ.items()
        if key in SAFE_ENVIRONMENT_KEYS
    }


def run_check(name: str, command: list[str], directory: Path) -> bool:
    print(f"Running {name}...")
    with LOG_PATH.open("a", encoding="utf-8") as log:
        log.write(f"\n--- {name} ---\n")
        log.flush()
        result = subprocess.run(
            command,
            cwd=directory,
            env=test_environment(),
            stdout=log,
            stderr=subprocess.STDOUT,
            check=False,
        )
    if result.returncode:
        print(f"{name} failed with exit code {result.returncode}")
        return False
    print(f"{name} passed")
    return True


def main() -> int:
    try:
        checks = validate_changes()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as exc:
        print(f"Could not validate agent changes: {exc}", file=sys.stderr)
        return 1

    LOG_PATH.parent.mkdir(parents=True, exist_ok=True)
    LOG_PATH.write_text("", encoding="utf-8")
    failed: list[str] = []

    if checks["backend"] and not run_check(
        "Backend test suite", ["devenv", "test"], ROOT
    ):
        failed.append("backend")
    if checks["ui"]:
        if not run_check("UI dependencies", ["npm", "ci"], ROOT / "ui"):
            failed.append("ui dependencies")
        elif not run_check("UI build", ["npm", "run", "build"], ROOT / "ui"):
            failed.append("UI build")
        if "ui dependencies" not in failed and not run_check(
            "UI lint", ["npm", "run", "lint"], ROOT / "ui"
        ):
            failed.append("UI lint")
    if checks["docs"]:
        if not run_check("Documentation dependencies", ["npm", "ci"], ROOT / "docs"):
            failed.append("documentation dependencies")
        elif not run_check(
            "Documentation build",
            ["npx", "antora", "--fetch", "antora-playbook.yml"],
            ROOT / "docs",
        ):
            failed.append("documentation build")

    if failed:
        print(f"Failed checks: {', '.join(failed)}", file=sys.stderr)
        try:
            feedback = LOG_PATH.read_text(encoding="utf-8")[-12_000:]
        except OSError:
            feedback = "(The validation log could not be read.)"
        print("\nValidation output for the agent repair attempt:\n")
        print(feedback)
        return 1
    print("All applicable repository checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
