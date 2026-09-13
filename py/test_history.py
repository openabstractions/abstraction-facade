import unittest
from types import SimpleNamespace
from abstraction.facade.client import LogHistory
from abstraction.logging import rec


class HistoryControls(unittest.TestCase):
    def test_refusal_progress_and_bounds(self):
        record = rec.Record(schema=1, time="2026-09-13T00:00:00Z", level=1, msg="test", attrs={})
        reader = LogHistory(None)
        def response(page):
            reader._client = SimpleNamespace(Read=lambda *args: page)
        good = rec.Page(outcome="page", records=[record], next="next", at_end=True)
        response(good)
        self.assertIs(reader.Read("cursor", 1, 65536), good)
        for page in (
            rec.Page(outcome="page", records=[record], next="cursor", at_end=True),
            rec.Page(outcome="page", records=[], next="next", at_end=False),
            rec.Page(outcome="gap", records=[record], next="cursor", at_end=False),
            rec.Page(outcome="gap", records=[], next="changed", at_end=False),
            rec.Page(outcome="page", records=[record, record], next="next", at_end=True),
        ):
            response(page)
            with self.assertRaises(ValueError):
                reader.Read("cursor", 1, 65536)
        response(good)
        with self.assertRaises(ValueError):
            reader.Read("cursor", 1, 1)
        with self.assertRaises(ValueError):
            reader.Read("cursor", True, 65536)


if __name__ == "__main__":
    unittest.main()
