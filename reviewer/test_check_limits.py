import unittest

from reviewer.check_limits import format_status


class OpenRouterStatusTests(unittest.TestCase):
    def test_formats_key_usage_and_rate_limit_without_exposing_key(self):
        status = format_status(
            {
                "data": {
                    "label": "reviewer",
                    "usage": 0.125,
                    "limit": 10,
                    "limit_remaining": 9.875,
                    "is_free_tier": False,
                    "rate_limit": {"requests": 50, "interval": "1 day"},
                    "key": "must-not-be-printed",
                }
            }
        )

        self.assertIn("Usage: $0.125", status)
        self.assertIn("Limit: $10", status)
        self.assertIn("Remaining: $9.875", status)
        self.assertIn("Free tier: no", status)
        self.assertIn("Rate limit: 50 requests per 1 day", status)
        self.assertNotIn("must-not-be-printed", status)

    def test_handles_unset_limits_and_optional_fields(self):
        status = format_status({"data": {"usage": 0}})

        self.assertIn("Limit: not set", status)
        self.assertIn("Remaining: not set", status)
        self.assertNotIn("Rate limit:", status)

    def test_rejects_unexpected_response_shape(self):
        with self.assertRaisesRegex(ValueError, "missing data"):
            format_status({})


if __name__ == "__main__":
    unittest.main()
