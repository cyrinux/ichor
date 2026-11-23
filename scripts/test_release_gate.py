"""Tests for release-gate.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import io
import json
import os
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("release_gate", os.path.join(HERE, "release-gate.py"))
release_gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_gate)


def run(run_id, conclusion="success", status="completed", attempt=1, name="CI"):
    """One run as `gh run list --json databaseId,conclusion,status,attempt,workflowName` prints it."""
    return [{"databaseId": run_id, "conclusion": conclusion, "status": status,
             "attempt": attempt, "workflowName": name}]


class DecideTest(unittest.TestCase):
    CASES = [
        # (name, android runs, ios runs, decision, rerun ids, blocked ids)
        ("both green", run(1), run(2), "release", [], []),
        ("ios failed on attempt 1", run(1), run(2, "failure"), "rerun", [2], []),
        ("ios failed on attempt 2", run(1), run(2, "failure", attempt=2), "blocked", [], [2]),
        ("android in progress", run(1, "", "in_progress"), run(2), "wait", [], []),
        ("a re-run in progress", run(1), run(2, "", "in_progress", attempt=2), "wait", [], []),
        ("ios missing", run(1), [], "wait", [], []),
        ("android green, ios cancelled", run(1), run(2, "cancelled"), "rerun", [2], []),
        ("ios timed out", run(1), run(2, "timed_out"), "rerun", [2], []),
        ("both failed on attempt 1", run(1, "failure"), run(2, "failure"), "rerun", [1, 2], []),
        ("blocked wins over rerun", run(1, "failure"), run(2, "failure", attempt=2), "blocked", [], [2]),
        ("rerun does not wait for the other", run(1, "failure"), run(2, "", "queued"), "rerun", [1], []),
    ]

    def test_rules(self):
        for name, android, ios, decision, rerun, blocked in self.CASES:
            with self.subTest(name):
                got = release_gate.decide({"android.yml": android, "ios.yml": ios})
                self.assertEqual(got["decision"], decision)
                self.assertEqual(got["rerun"], rerun)
                self.assertEqual(got["blocked"], blocked)

    def test_blocked_reason_names_the_workflow(self):
        got = release_gate.decide({"android.yml": run(1), "ios.yml": run(2, "failure", attempt=2)})
        self.assertIn("ios.yml: failure on attempt 2", got["reason"])

    def test_missing_attempt_counts_as_the_first(self):
        runs = [{"databaseId": 7, "conclusion": "failure", "status": "completed"}]
        self.assertEqual(release_gate.decide({"ios.yml": runs})["decision"], "rerun")

    def test_no_workflow_waits(self):
        self.assertEqual(release_gate.decide({})["decision"], "wait")


class MainTest(unittest.TestCase):
    def main_with(self, stdin):
        out = io.StringIO()
        with mock.patch("sys.stdin", io.StringIO(stdin)), mock.patch("sys.stdout", out), \
                mock.patch("sys.stderr", io.StringIO()):
            code = release_gate.main()
        return code, out.getvalue()

    def test_prints_one_json_decision(self):
        code, out = self.main_with(json.dumps({"android.yml": run(1), "ios.yml": run(2)}))
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out)["decision"], "release")

    def test_rejects_bad_input(self):
        self.assertEqual(self.main_with("not json")[0], 2)
        self.assertEqual(self.main_with("[]")[0], 2)


if __name__ == "__main__":
    unittest.main()
