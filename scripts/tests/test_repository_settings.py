import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("settings", Path(__file__).resolve().parents[1] / "repository-settings.py")
settings = importlib.util.module_from_spec(spec)
spec.loader.exec_module(settings)


class SettingsTests(unittest.TestCase):
    def test_server_defaults_do_not_hide_a_missing_requirement(self):
        actual = {"enforcement": "active", "rules": [{"type": "non_fast_forward"}], "id": 1}
        self.assertTrue(settings.matches(actual, {"enforcement": "active"}))
        self.assertFalse(settings.matches(actual, {"rules": [{"type": "deletion"}]}))

    def test_list_order_is_irrelevant(self):
        self.assertTrue(settings.matches([{"type": "update"}, {"type": "deletion"}],
            [{"type": "deletion"}, {"type": "update"}]))

    def test_extra_bypass_actor_is_rejected(self):
        self.assertFalse(settings.matches({"bypass_actors": [{"actor_id": 1}]}, {"bypass_actors": []}))

    def test_disabled_policy_is_rejected(self):
        self.assertFalse(settings.matches({"enforcement": "disabled"}, {"enforcement": "active"}))
