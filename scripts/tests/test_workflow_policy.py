import fnmatch
import json
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class WorkflowPolicyTests(unittest.TestCase):
    def test_actions_are_pinned_and_allowlisted(self):
        policy = json.loads((ROOT / ".github/repository/selected-actions.json").read_text())
        count = 0
        for path in (ROOT / ".github/workflows").glob("*.yml"):
            for reference in re.findall(r"uses:\s*([^\s#]+)", path.read_text()):
                count += 1
                self.assertRegex(reference, r"^[\w.-]+/[\w./-]+@[0-9a-f]{40}$", path.name)
                self.assertTrue(any(fnmatch.fnmatchcase(reference, pattern) for pattern in policy["patterns_allowed"]), reference)
        self.assertGreater(count, 0)

    def test_required_checks_exist_in_workflows(self):
        policy = json.loads((ROOT / ".github/repository/default.json").read_text())
        contexts = next(rule for rule in policy["rules"] if rule["type"] == "required_status_checks")
        workflow_text = "\n".join(path.read_text() for path in (ROOT / ".github/workflows").glob("*.yml"))
        for check in contexts["parameters"]["required_status_checks"]:
            self.assertIn("name: " + check["context"] + "\n", workflow_text)
            self.assertEqual(check["integration_id"], 15368)

    def test_go_tags_are_immutable_without_bypasses(self):
        policy = json.loads((ROOT / ".github/repository/release-tags.json").read_text())
        self.assertEqual(policy["bypass_actors"], [])
        self.assertIn("refs/tags/v*", policy["conditions"]["ref_name"]["include"])
        self.assertTrue({"update", "deletion", "non_fast_forward"}.issubset({rule["type"] for rule in policy["rules"]}))
