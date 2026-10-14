import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from reviewer import issue_agent
from reviewer import run_issue_checks


class PathPolicyTests(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.temp_dir.name)
        (self.root / ".github").mkdir()
        (self.root / ".github/copilot-instructions.md").write_text(
            "Repository guidance", encoding="utf-8"
        )
        (self.root / "services/uploader").mkdir(parents=True)
        (self.root / "services/uploader/main.go").write_text(
            "package main\n", encoding="utf-8"
        )
        self.root_patch = patch.object(issue_agent, "ROOT", self.root)
        self.root_patch.start()

    def tearDown(self):
        self.root_patch.stop()
        self.temp_dir.cleanup()

    def test_read_only_instructions_are_readable(self):
        self.assertEqual(
            issue_agent.read_file(".github/copilot-instructions.md"),
            "Repository guidance",
        )

    def test_source_file_can_be_updated(self):
        result = issue_agent.write_file("services/uploader/main.go", "package main\n\n")
        self.assertIn("Wrote services/uploader/main.go", result)
        self.assertEqual(
            (self.root / "services/uploader/main.go").read_text(encoding="utf-8"),
            "package main\n\n",
        )

    def test_new_source_file_can_be_created(self):
        issue_agent.write_file("ui/src/new.ts", "export const value = 1;\n")
        self.assertTrue((self.root / "ui/src/new.ts").is_file())

    def test_large_existing_file_cannot_be_overwritten(self):
        large_file = self.root / "services/uploader/large.go"
        large_file.write_text("x" * (issue_agent.MAX_FILE_CHARS + 1), encoding="utf-8")
        with self.assertRaises(issue_agent.ToolError):
            issue_agent.write_file("services/uploader/large.go", "replacement")

    def test_protected_and_out_of_scope_paths_are_rejected(self):
        for path in (
            ".github/workflows/agent.yml",
            "reviewer/reviewer.py",
            "taskfile.yml",
            "ui/package.json",
            "../outside.txt",
            "/tmp/outside.txt",
        ):
            with self.subTest(path=path):
                with self.assertRaises(issue_agent.ToolError):
                    issue_agent.normalized_path(path, writing=True)

    def test_read_only_guidance_cannot_be_written(self):
        with self.assertRaises(issue_agent.ToolError):
            issue_agent.write_file(
                ".github/copilot-instructions.md", "modified instructions"
            )

    def test_symlink_paths_are_rejected(self):
        (self.root / "services/uploader/linked.txt").symlink_to("/etc/passwd")
        with self.assertRaises(issue_agent.ToolError):
            issue_agent.read_file("services/uploader/linked.txt")

    def test_agent_executes_scoped_tool_calls_and_saves_summary(self):
        (self.root / "services/uploader/main.go").unlink()
        responses = [
            {
                "tool_calls": [
                    {
                        "id": "write-1",
                        "function": {
                            "name": "write_file",
                            "arguments": json.dumps(
                                {
                                    "path": "services/uploader/main.go",
                                    "content": "package main\n",
                                }
                            ),
                        },
                    }
                ]
            },
            {"content": "Implemented the requested change."},
        ]
        with (
            patch.object(issue_agent, "ROOT", self.root),
            patch.dict(
                "os.environ",
                {
                    "OPENROUTER_API_KEY": "test-key",
                    "OPENROUTER_MODEL": "provider/model",
                    "ISSUE_TITLE": "Implement a small change",
                    "ISSUE_BODY": "Update the module.",
                    "AGENT_SUMMARY_FILE": str(self.root / "summary.md"),
                },
                clear=True,
            ),
            patch.object(issue_agent, "openrouter_request", side_effect=responses),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            issue_agent.run_agent()

        self.assertEqual(
            (self.root / "services/uploader/main.go").read_text(encoding="utf-8"),
            "package main\n",
        )
        self.assertEqual(
            (self.root / "summary.md").read_text(encoding="utf-8"),
            "Implemented the requested change.\n",
        )


class ValidationEnvironmentTests(unittest.TestCase):
    def test_agent_and_github_secrets_are_not_passed_to_checks(self):
        with patch.dict(
            "os.environ",
            {
                "PATH": "/usr/bin",
                "OPENROUTER_API_KEY": "secret",
                "GITHUB_TOKEN": "token",
                "ACTIONS_RUNTIME_TOKEN": "runtime",
            },
            clear=True,
        ):
            self.assertEqual(run_issue_checks.test_environment(), {"PATH": "/usr/bin"})


if __name__ == "__main__":
    unittest.main()
