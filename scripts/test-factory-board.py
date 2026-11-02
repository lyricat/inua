#!/usr/bin/env python3
"""Regression tests for the factory board's current item format."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("board", Path(__file__).with_name("factory-board.py"))
board = importlib.util.module_from_spec(spec)
spec.loader.exec_module(board)


class BoardTest(unittest.TestCase):
    def test_question_waits_for_owner(self):
        _, _, sections, problems = board.parse(
            "# Request\n\n## factory · question · 2026-11-02 12:00\nWhich?\n"
            "## visitor · 2026-11-02 12:01\nYes\n")
        self.assertEqual(problems, [])
        self.assertEqual(board.turn("new", sections, {"owner"}), "you")
        sections.append({"who": "owner", "kind": ""})
        self.assertEqual(board.turn("new", sections, {"owner"}), "agent")
        self.assertEqual(board.turn("done", sections, {"owner"}), "")

    def test_plan_does_not_wait(self):
        _, _, sections, problems = board.parse(
            "# Request\n\n## factory · plan · 2026-11-02 12:00\nBuild\n")
        self.assertEqual(problems, [])
        self.assertEqual(board.turn("new", sections, {"owner"}), "agent")

    def test_review_section(self):
        _, _, sections, problems = board.parse(
            "# Request\n\n## reviewer · review · 2026-11-02 20:00\n"
            "head: abc123\nverdict: pass\n")
        self.assertEqual(problems, [])
        self.assertEqual(sections, [{
            "who": "reviewer", "kind": "review", "time": "2026-11-02 20:00",
            "body": "head: abc123\nverdict: pass",
        }])

    def test_formats_and_visible_folders(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / ".factory"
            root.mkdir()
            (root / "factory.toml").write_text('owners = ["owner"]\nprefix = "FT"\n')
            for folder, name in [("new", "request.md"), ("review", "AB16-prefixed.md"),
                                 ("ready", "0053-legacy.md"), ("stop", "FT99-parked.md"),
                                 ("building", "0055-old.md"), ("review", "unnumbered.md")]:
                (root / folder).mkdir(exist_ok=True)
                (root / folder / name).write_text(
                    "# <unsafe>\n\n## factory · question · 2026-11-02 12:00\n<script>\n")
            with patch.object(board, "git", return_value=""):
                cfg, name, items = board.load(tmp)
            self.assertEqual([it["label"] for it in items], ["new", "AB16", "FT53"])
            page = board.render(cfg, name, items)
            self.assertEqual(board.FOLDERS, ["new", "review", "ready", "done"])
            self.assertNotIn("<unsafe>", page)
            self.assertIn("&lt;script&gt;", page)
            self.assertIn('id="item-request"', page)
            self.assertIn("answer</span>", page)


if __name__ == "__main__":
    unittest.main()
