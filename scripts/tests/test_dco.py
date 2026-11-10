import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("dco", Path(__file__).resolve().parents[1] / "check-dco.py")
dco = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dco)


class DCOTests(unittest.TestCase):
    def test_author_signoff_is_accepted(self):
        self.assertTrue(dco.signed_off("Fixture Author <author@example.invalid>",
            "feat(evidence): add fixture\n\nSigned-off-by: Fixture Author <author@example.invalid>\n"))

    def test_different_author_is_rejected(self):
        self.assertFalse(dco.signed_off("Fixture Author <author@example.invalid>",
            "feat(evidence): add fixture\n\nSigned-off-by: Other Author <other@example.invalid>\n"))

    def test_body_text_is_not_a_trailer(self):
        self.assertFalse(dco.signed_off("Fixture Author <author@example.invalid>",
            "feat(evidence): add fixture\n\nSigned-off-by: Fixture Author <author@example.invalid>\n\nThis is the body.\n"))

    def test_missing_signoff_is_rejected(self):
        self.assertFalse(dco.signed_off("Fixture Author <author@example.invalid>", "feat(evidence): add fixture\n"))
